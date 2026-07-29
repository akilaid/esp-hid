// HID report map and input-report state: keyboard (report ID 1, 6KRO) and
// mouse (report ID 3, 5 buttons + int8 x/y/wheel/AC-pan).
#pragma once

#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"
#include "esp_hidd.h"

#ifdef __cplusplus
extern "C" {
#endif

#define HID_REPORT_ID_KEYBOARD 1
#define HID_REPORT_ID_MOUSE 3

extern const uint8_t hid_report_map[];
extern const size_t hid_report_map_len;

void hid_reports_init(esp_hidd_dev_t *dev);

// All senders return ESP_ERR_INVALID_STATE while no BLE host is connected
// (input is dropped, matching legacy behavior), or the esp_hidd send error.
esp_err_t hid_move(int16_t dx, int16_t dy);
esp_err_t hid_buttons(uint8_t mask);              // absolute 5-bit state
esp_err_t hid_wheel(int8_t vertical, int8_t horizontal);
esp_err_t hid_key_down(uint8_t usage);            // 0xE0..0xE7 = modifiers
esp_err_t hid_key_up(uint8_t usage);
esp_err_t hid_release_all(void);

#ifdef __cplusplus
}
#endif
