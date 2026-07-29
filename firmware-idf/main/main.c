// ESP-HID bridge: replays host input commands (binary protocol over the
// native USB Serial/JTAG CDC port) as BLE HID reports to a paired device.
#include <string.h>

#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "nvs_flash.h"
#include "sdkconfig.h"

#include "ble_hid.h"
#include "hid_reports.h"
#include "protocol.h"
#include "status_led.h"
#include "usb_link.h"

static const char *TAG = "bridge";

#define FW_MAJOR 1
#define FW_MINOR 0
#define FW_PATCH 0

// caps bit0 = mouse, bit1 = keyboard.
#define FW_CAPS 0x0003

static void send_hello(void)
{
    uint8_t payload[6] = {
        PROTO_VERSION, FW_MAJOR, FW_MINOR, FW_PATCH,
        FW_CAPS & 0xFF, FW_CAPS >> 8,
    };
    usb_link_send(PROTO_HELLO, payload, sizeof(payload));
}

static void send_ble_state(void)
{
    uint8_t payload[3] = {
        (uint8_t)ble_hid_state(),
        ble_hid_last_disconnect_reason(),
        ble_hid_bond_count(),
    };
    usb_link_send(PROTO_BLE_STATE, payload, sizeof(payload));
}

static void on_ble_state(ble_hid_state_t state, uint8_t reason)
{
    (void)reason;
    status_led_set_connected(state == BLE_HID_CONNECTED);
    send_ble_state();
}

static void report_input_result(esp_err_t err)
{
    if (err == ESP_ERR_INVALID_STATE) {
        usb_link_send_error(PROTO_ERR_NOT_CONNECTED_DROP, (uint8_t)ble_hid_state());
    } else if (err != ESP_OK) {
        usb_link_send_error(PROTO_ERR_HID_SEND_FAIL, 0);
    }
}

static int16_t read_i16(const uint8_t *p)
{
    return (int16_t)((uint16_t)p[0] | ((uint16_t)p[1] << 8));
}

static void dispatch(const proto_frame_t *frame)
{
    switch (frame->type) {
    case PROTO_PING:
        usb_link_send(PROTO_PONG, frame->payload, frame->len);
        break;

    case PROTO_GET_STATUS:
        send_hello();
        send_ble_state();
        break;

    case PROTO_MOVE:
        if (frame->len == 4) {
            report_input_result(
                hid_move(read_i16(&frame->payload[0]), read_i16(&frame->payload[2])));
        }
        break;

    case PROTO_BUTTONS:
        if (frame->len == 1) {
            report_input_result(hid_buttons(frame->payload[0]));
        }
        break;

    case PROTO_WHEEL:
        if (frame->len == 2) {
            report_input_result(
                hid_wheel((int8_t)frame->payload[0], (int8_t)frame->payload[1]));
        }
        break;

    case PROTO_KEY_DOWN:
        if (frame->len == 1) {
            report_input_result(hid_key_down(frame->payload[0]));
        }
        break;

    case PROTO_KEY_UP:
        if (frame->len == 1) {
            report_input_result(hid_key_up(frame->payload[0]));
        }
        break;

    case PROTO_RELEASE_ALL: {
        esp_err_t err = hid_release_all();
        // Dropping release-all while disconnected is harmless: there is no
        // stuck state on an absent peer.
        if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
            usb_link_send_error(PROTO_ERR_HID_SEND_FAIL, 0);
        }
        break;
    }

    case PROTO_CLEAR_BONDS:
        if (ble_hid_clear_bonds() == ESP_OK) {
            uint8_t acked = PROTO_CLEAR_BONDS;
            usb_link_send(PROTO_ACK, &acked, 1);
            send_ble_state();
        } else {
            usb_link_send_error(PROTO_ERR_HID_SEND_FAIL, PROTO_CLEAR_BONDS);
        }
        break;

    default:
        usb_link_send_error(PROTO_ERR_UNKNOWN_TYPE, frame->type);
        break;
    }
}

void app_main(void)
{
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(err);

    status_led_init();

    QueueHandle_t frames;
    ESP_ERROR_CHECK(usb_link_init(&frames));

    ble_hid_callbacks_t callbacks = { .on_state = on_ble_state };
    ESP_ERROR_CHECK(ble_hid_init(&callbacks));
    hid_reports_init(ble_hid_dev());

    send_hello();
    send_ble_state();
    ESP_LOGI(TAG, "bridge ready, fw %d.%d.%d", FW_MAJOR, FW_MINOR, FW_PATCH);

    proto_frame_t frame;
    for (;;) {
        if (xQueueReceive(frames, &frame, portMAX_DELAY) == pdTRUE) {
            dispatch(&frame);
        }
    }
}
