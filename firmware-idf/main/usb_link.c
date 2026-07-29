#include "usb_link.h"

#include <string.h>

#include "driver/usb_serial_jtag.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/task.h"
#include "sdkconfig.h"

static const char *TAG = "usb_link";

#define RX_TASK_STACK 4096
#define RX_TASK_PRIO 10
#define FRAME_QUEUE_DEPTH 64

static QueueHandle_t s_frame_queue;
static SemaphoreHandle_t s_tx_mutex;

// One send-timestamp per protocol error code (codes are small ints).
static int64_t s_error_last_us[8];

void usb_link_send(uint8_t type, const void *payload, uint8_t len)
{
    uint8_t frame[PROTO_MAX_FRAME];
    size_t n = proto_encode(type, payload, len, frame);
    if (n == 0) {
        return;
    }
    xSemaphoreTake(s_tx_mutex, portMAX_DELAY);
    // Timeout rather than block forever: if the host isn't reading, drop.
    usb_serial_jtag_write_bytes(frame, n, pdMS_TO_TICKS(20));
    xSemaphoreGive(s_tx_mutex);
}

void usb_link_send_error(uint8_t code, uint8_t detail)
{
    if (code < sizeof(s_error_last_us) / sizeof(s_error_last_us[0])) {
        int64_t now = esp_timer_get_time();
        if (now - s_error_last_us[code] < 1000000) {
            return;
        }
        s_error_last_us[code] = now;
    }
    uint8_t payload[2] = { code, detail };
    usb_link_send(PROTO_ERROR, payload, sizeof(payload));
}

static void rx_task(void *arg)
{
    (void)arg;
    proto_decoder_t decoder;
    proto_decoder_reset(&decoder);
    uint8_t buf[256];
    proto_frame_t frame;

    for (;;) {
        int n = usb_serial_jtag_read_bytes(buf, sizeof(buf), pdMS_TO_TICKS(100));
        for (int i = 0; i < n; i++) {
            proto_decode_result_t r = proto_decoder_feed(&decoder, buf[i], &frame);
            switch (r) {
            case PROTO_DECODE_FRAME:
                if (xQueueSend(s_frame_queue, &frame, 0) != pdTRUE) {
                    ESP_LOGW(TAG, "frame queue full, dropping type 0x%02X", frame.type);
                }
                break;
            case PROTO_DECODE_ERR_BAD_CRC:
                usb_link_send_error(PROTO_ERR_BAD_CRC, 0);
                break;
            case PROTO_DECODE_ERR_BAD_LEN:
                usb_link_send_error(PROTO_ERR_BAD_LEN, 0);
                break;
            default:
                break;
            }
        }
    }
}

#if CONFIG_BRIDGE_LOG_FRAMES
static vprintf_like_t s_prev_vprintf;

static int log_frame_vprintf(const char *fmt, va_list args)
{
    char line[128];
    va_list copy;
    va_copy(copy, args);
    int written = vsnprintf(line, sizeof(line), fmt, copy);
    va_end(copy);
    if (written > 0) {
        size_t len = strnlen(line, sizeof(line));
        // Strip the trailing newline; frames are already delimited.
        while (len > 0 && (line[len - 1] == '\n' || line[len - 1] == '\r')) {
            len--;
        }
        // Long lines span several LOG frames; the reader just concatenates.
        for (size_t off = 0; off < len; off += PROTO_MAX_PAYLOAD) {
            size_t chunk = len - off;
            if (chunk > PROTO_MAX_PAYLOAD) {
                chunk = PROTO_MAX_PAYLOAD;
            }
            usb_link_send(PROTO_LOG, &line[off], (uint8_t)chunk);
        }
    }
    return s_prev_vprintf ? s_prev_vprintf(fmt, args) : written;
}
#endif

esp_err_t usb_link_init(QueueHandle_t *out_frame_queue)
{
    s_tx_mutex = xSemaphoreCreateMutex();
    s_frame_queue = xQueueCreate(FRAME_QUEUE_DEPTH, sizeof(proto_frame_t));
    if (s_tx_mutex == NULL || s_frame_queue == NULL) {
        return ESP_ERR_NO_MEM;
    }

    usb_serial_jtag_driver_config_t cfg = {
        .tx_buffer_size = 1024,
        .rx_buffer_size = 1024,
    };
    esp_err_t err = usb_serial_jtag_driver_install(&cfg);
    if (err != ESP_OK) {
        return err;
    }

    if (xTaskCreate(rx_task, "usb_rx", RX_TASK_STACK, NULL, RX_TASK_PRIO, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }

#if CONFIG_BRIDGE_LOG_FRAMES
    s_prev_vprintf = esp_log_set_vprintf(log_frame_vprintf);
#endif

    *out_frame_queue = s_frame_queue;
    return ESP_OK;
}
