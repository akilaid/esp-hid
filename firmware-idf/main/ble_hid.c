#include "ble_hid.h"

#include <string.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "host/ble_gap.h"
#include "host/ble_hs.h"
#include "host/ble_hs_id.h"
#include "host/ble_store.h"
#include "host/util/util.h"
#include "nimble/nimble_port.h"
#include "nimble/nimble_port_freertos.h"
#include "sdkconfig.h"
#include "services/gap/ble_svc_gap.h"

#include "hid_reports.h"

// Not declared in any public NimBLE header; the official esp_hid_device
// example forward-declares it the same way.
void ble_store_config_init(void);

static const char *TAG = "ble_hid";

#define APPEARANCE_HID_KEYBOARD 0x03C1
#define HID_SERVICE_UUID 0x1812

// Same identity values the proven legacy firmware reported via PnP.
#define BRIDGE_VID 0xE502
#define BRIDGE_PID 0xA111
#define BRIDGE_VERSION 0x0210

// Low-latency connection parameters (1.25 ms units / 10 ms units): 7.5-15 ms
// interval, no latency, 2 s supervision timeout. This is what makes mouse
// motion feel smooth; carried over from the legacy firmware.
#define CONN_ITVL_MIN 6
#define CONN_ITVL_MAX 12
#define CONN_LATENCY 0
#define CONN_TIMEOUT 200

static esp_hidd_dev_t *s_dev;
static ble_hid_callbacks_t s_callbacks;
static volatile ble_hid_state_t s_state = BLE_HID_IDLE;
static volatile uint8_t s_last_reason;
static uint8_t s_own_addr_type;
static esp_timer_handle_t s_adv_retry_timer;

// A failed incoming connection (e.g. a peer with a stale bond aborting
// encryption) can leave its slot occupied briefly; an immediate re-advertise
// then fails with ENOMEM and no further event would ever retry it. Any
// advertising failure schedules a retry instead of giving up.
#define ADV_RETRY_DELAY_US (500 * 1000)

static void set_state(ble_hid_state_t state)
{
    if (state == s_state) {
        return;
    }
    s_state = state;
    if (s_callbacks.on_state != NULL) {
        s_callbacks.on_state(state, s_last_reason);
    }
}

static int gap_event_cb(struct ble_gap_event *event, void *arg);

static void start_advertising(void)
{
    struct ble_hs_adv_fields fields = { 0 };
    fields.flags = BLE_HS_ADV_F_DISC_GEN | BLE_HS_ADV_F_BREDR_UNSUP;
    fields.appearance = APPEARANCE_HID_KEYBOARD;
    fields.appearance_is_present = 1;
    static const ble_uuid16_t hid_uuid = BLE_UUID16_INIT(HID_SERVICE_UUID);
    fields.uuids16 = &hid_uuid;
    fields.num_uuids16 = 1;
    fields.uuids16_is_complete = 1;

    int rc = ble_gap_adv_set_fields(&fields);
    if (rc != 0) {
        ESP_LOGE(TAG, "adv_set_fields failed: %d", rc);
        return;
    }

    struct ble_hs_adv_fields rsp = { 0 };
    rsp.name = (const uint8_t *)CONFIG_BRIDGE_DEVICE_NAME;
    rsp.name_len = strlen(CONFIG_BRIDGE_DEVICE_NAME);
    rsp.name_is_complete = 1;
    rc = ble_gap_adv_rsp_set_fields(&rsp);
    if (rc != 0) {
        ESP_LOGE(TAG, "adv_rsp_set_fields failed: %d", rc);
        return;
    }

    struct ble_gap_adv_params params = { 0 };
    params.conn_mode = BLE_GAP_CONN_MODE_UND;
    params.disc_mode = BLE_GAP_DISC_MODE_GEN;
    // 30-50 ms advertising interval: quick discovery without burning power.
    params.itvl_min = 0x30;
    params.itvl_max = 0x50;

    rc = ble_gap_adv_start(s_own_addr_type, NULL, BLE_HS_FOREVER, &params,
                           gap_event_cb, NULL);
    if (rc == 0 || rc == BLE_HS_EALREADY) {
        ESP_LOGI(TAG, "advertising as \"%s\"", CONFIG_BRIDGE_DEVICE_NAME);
        esp_timer_stop(s_adv_retry_timer);
        set_state(BLE_HID_ADVERTISING);
    } else {
        ESP_LOGE(TAG, "adv_start failed: %d, retrying", rc);
        set_state(BLE_HID_IDLE);
        esp_timer_stop(s_adv_retry_timer);
        esp_timer_start_once(s_adv_retry_timer, ADV_RETRY_DELAY_US);
    }
}

static void adv_retry_cb(void *arg)
{
    (void)arg;
    if (s_state != BLE_HID_CONNECTED) {
        start_advertising();
    }
}

static int gap_event_cb(struct ble_gap_event *event, void *arg)
{
    (void)arg;
    switch (event->type) {
    case BLE_GAP_EVENT_CONNECT:
        if (event->connect.status != 0) {
            // The failed connection's slot may not be free yet; retry on the
            // timer rather than hitting ENOMEM immediately.
            ESP_LOGW(TAG, "connect failed (status %d), re-advertising",
                     event->connect.status);
            esp_timer_stop(s_adv_retry_timer);
            esp_timer_start_once(s_adv_retry_timer, ADV_RETRY_DELAY_US);
            break;
        }
        ESP_LOGI(TAG, "GAP connected (handle %d)", event->connect.conn_handle);
        // Request the low-latency interval immediately; the HID-ready state
        // is reported when the host subscribes (ESP_HIDD_CONNECT_EVENT).
        {
            struct ble_gap_upd_params params = {
                .itvl_min = CONN_ITVL_MIN,
                .itvl_max = CONN_ITVL_MAX,
                .latency = CONN_LATENCY,
                .supervision_timeout = CONN_TIMEOUT,
            };
            int rc = ble_gap_update_params(event->connect.conn_handle, &params);
            if (rc != 0) {
                ESP_LOGW(TAG, "conn param update failed: %d", rc);
            }
        }
        break;

    case BLE_GAP_EVENT_DISCONNECT:
        s_last_reason = (uint8_t)event->disconnect.reason;
        ESP_LOGI(TAG, "GAP disconnected (reason 0x%02X)", s_last_reason);
        start_advertising();
        break;

    case BLE_GAP_EVENT_ADV_COMPLETE:
        // Advertising stopped without a connection (shouldn't happen with
        // BLE_HS_FOREVER, but never stay dark).
        if (s_state != BLE_HID_CONNECTED) {
            esp_timer_stop(s_adv_retry_timer);
            esp_timer_start_once(s_adv_retry_timer, ADV_RETRY_DELAY_US);
        }
        break;

    case BLE_GAP_EVENT_REPEAT_PAIRING: {
        // The peer lost or replaced its keys (e.g. user re-paired after a
        // bond wipe on one side). Delete our stale bond and let it pair
        // fresh instead of failing encryption forever.
        struct ble_gap_conn_desc desc;
        if (ble_gap_conn_find(event->repeat_pairing.conn_handle, &desc) == 0) {
            ble_store_util_delete_peer(&desc.peer_id_addr);
            ESP_LOGW(TAG, "stale bond deleted for re-pairing peer");
        }
        return BLE_GAP_REPEAT_PAIRING_RETRY;
    }

    default:
        break;
    }
    return 0;
}

static void on_sync(void)
{
    int rc = ble_hs_util_ensure_addr(0);
    if (rc != 0) {
        ESP_LOGE(TAG, "ensure_addr failed: %d", rc);
        return;
    }
    rc = ble_hs_id_infer_auto(0, &s_own_addr_type);
    if (rc != 0) {
        ESP_LOGE(TAG, "infer_auto failed: %d", rc);
        return;
    }
    start_advertising();
}

static void on_reset(int reason)
{
    ESP_LOGW(TAG, "NimBLE host reset, reason %d", reason);
    set_state(BLE_HID_IDLE);
}

static void hidd_event_cb(void *handler_args, esp_event_base_t base,
                          int32_t id, void *event_data)
{
    (void)handler_args;
    (void)base;
    (void)event_data;
    switch ((esp_hidd_event_t)id) {
    case ESP_HIDD_START_EVENT:
        ESP_LOGI(TAG, "HID device stack started");
        break;
    case ESP_HIDD_CONNECT_EVENT:
        // Authoritative "input can flow now": the host has subscribed.
        ESP_LOGI(TAG, "HID host connected");
        set_state(BLE_HID_CONNECTED);
        break;
    case ESP_HIDD_DISCONNECT_EVENT:
        ESP_LOGI(TAG, "HID host disconnected");
        // GAP callback restarts advertising; state follows it.
        if (s_state == BLE_HID_CONNECTED) {
            set_state(BLE_HID_ADVERTISING);
        }
        break;
    default:
        break;
    }
}

static void host_task(void *arg)
{
    (void)arg;
    nimble_port_run();
    nimble_port_freertos_deinit();
}

esp_err_t ble_hid_init(const ble_hid_callbacks_t *callbacks)
{
    if (callbacks != NULL) {
        s_callbacks = *callbacks;
    }

    const esp_timer_create_args_t retry_args = {
        .callback = adv_retry_cb,
        .name = "adv_retry",
    };
    esp_err_t err = esp_timer_create(&retry_args, &s_adv_retry_timer);
    if (err != ESP_OK) {
        return err;
    }

    err = nimble_port_init();
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "nimble_port_init failed: %d", err);
        return err;
    }

    // Just Works pairing with bonding and LE Secure Connections; identical
    // security posture to the legacy firmware.
    ble_hs_cfg.sm_io_cap = BLE_HS_IO_NO_INPUT_OUTPUT;
    ble_hs_cfg.sm_bonding = 1;
    ble_hs_cfg.sm_mitm = 0;
    ble_hs_cfg.sm_sc = 1;
    ble_hs_cfg.sm_our_key_dist = BLE_SM_PAIR_KEY_DIST_ID | BLE_SM_PAIR_KEY_DIST_ENC;
    ble_hs_cfg.sm_their_key_dist |= BLE_SM_PAIR_KEY_DIST_ID | BLE_SM_PAIR_KEY_DIST_ENC;
    ble_hs_cfg.sync_cb = on_sync;
    ble_hs_cfg.reset_cb = on_reset;
    ble_hs_cfg.store_status_cb = ble_store_util_status_rr;

    esp_hid_raw_report_map_t report_maps[] = {
        { .data = hid_report_map, .len = (uint16_t)hid_report_map_len },
    };
    esp_hid_device_config_t config = {
        .vendor_id = BRIDGE_VID,
        .product_id = BRIDGE_PID,
        .version = BRIDGE_VERSION,
        .device_name = CONFIG_BRIDGE_DEVICE_NAME,
        .manufacturer_name = CONFIG_BRIDGE_MANUFACTURER,
        .serial_number = "1",
        .report_maps = report_maps,
        .report_maps_len = 1,
    };
    // Chains our sync_cb/reset_cb and registers the HID GATT services.
    err = esp_hidd_dev_init(&config, ESP_HID_TRANSPORT_BLE, hidd_event_cb, &s_dev);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "esp_hidd_dev_init failed: %d", err);
        return err;
    }
    esp_hidd_dev_battery_set(s_dev, 100);

    ble_svc_gap_device_name_set(CONFIG_BRIDGE_DEVICE_NAME);
    ble_svc_gap_device_appearance_set(APPEARANCE_HID_KEYBOARD);

    ble_store_config_init();

    nimble_port_freertos_init(host_task);
    return ESP_OK;
}

esp_hidd_dev_t *ble_hid_dev(void)
{
    return s_dev;
}

ble_hid_state_t ble_hid_state(void)
{
    return s_state;
}

uint8_t ble_hid_last_disconnect_reason(void)
{
    return s_last_reason;
}

uint8_t ble_hid_bond_count(void)
{
    int count = 0;
    if (ble_store_util_count(BLE_STORE_OBJ_TYPE_OUR_SEC, &count) != 0) {
        return 0;
    }
    return (uint8_t)count;
}

esp_err_t ble_hid_clear_bonds(void)
{
    // Drop an active connection first: its keys are about to vanish and the
    // peer must go through fresh pairing.
    if (s_state == BLE_HID_CONNECTED) {
        // Find and terminate every open connection (we allow only one).
        for (uint16_t handle = 0; handle <= 8; handle++) {
            struct ble_gap_conn_desc desc;
            if (ble_gap_conn_find(handle, &desc) == 0) {
                ble_gap_terminate(handle, BLE_ERR_REM_USER_CONN_TERM);
            }
        }
    }
    int rc = ble_store_clear();
    ESP_LOGI(TAG, "bonds cleared (rc %d)", rc);
    return rc == 0 ? ESP_OK : ESP_FAIL;
}
