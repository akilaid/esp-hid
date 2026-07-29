// USB Serial/JTAG transport: owns the driver, an RX decode task, and
// mutex-guarded frame TX. Decode errors are reported to the host as ERROR
// frames (rate-limited) directly by this module.
#pragma once

#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "protocol.h"

#ifdef __cplusplus
extern "C" {
#endif

// Installs the driver and starts the RX task. Complete valid frames are
// pushed to the returned queue (of proto_frame_t).
esp_err_t usb_link_init(QueueHandle_t *out_frame_queue);

// Encodes and writes one frame. Safe to call from any task.
void usb_link_send(uint8_t type, const void *payload, uint8_t len);

// Sends an ERROR frame, rate-limited to one per second per code.
void usb_link_send_error(uint8_t code, uint8_t detail);

#ifdef __cplusplus
}
#endif
