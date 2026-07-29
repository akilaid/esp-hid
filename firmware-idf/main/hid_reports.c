#include "hid_reports.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

// Report map carried over from the proven legacy descriptor
// (firmware/libraries/ESP32-BLE-Combo), minus the never-used consumer/media
// collection (old report ID 2). Report IDs kept identical (1 keyboard,
// 3 mouse) so phone-side behavior matches the map that already worked.
const uint8_t hid_report_map[] = {
    // --- Keyboard, Report ID 1 ---
    0x05, 0x01,        // Usage Page (Generic Desktop)
    0x09, 0x06,        // Usage (Keyboard)
    0xA1, 0x01,        // Collection (Application)
    0x85, 0x01,        //   Report ID (1)
    0x05, 0x07,        //   Usage Page (Keyboard/Keypad)
    0x19, 0xE0,        //   Usage Minimum (LeftControl)
    0x29, 0xE7,        //   Usage Maximum (Right GUI)
    0x15, 0x00,        //   Logical Minimum (0)
    0x25, 0x01,        //   Logical Maximum (1)
    0x75, 0x01,        //   Report Size (1)
    0x95, 0x08,        //   Report Count (8)      -> modifier bits
    0x81, 0x02,        //   Input (Data,Var,Abs)
    0x95, 0x01,        //   Report Count (1)
    0x75, 0x08,        //   Report Size (8)       -> reserved byte
    0x81, 0x01,        //   Input (Const)
    0x95, 0x05,        //   Report Count (5)      -> LED bits (output)
    0x75, 0x01,        //   Report Size (1)
    0x05, 0x08,        //   Usage Page (LEDs)
    0x19, 0x01,        //   Usage Minimum (Num Lock)
    0x29, 0x05,        //   Usage Maximum (Kana)
    0x91, 0x02,        //   Output (Data,Var,Abs)
    0x95, 0x01,        //   Report Count (1)
    0x75, 0x03,        //   Report Size (3)       -> LED padding
    0x91, 0x01,        //   Output (Const)
    0x95, 0x06,        //   Report Count (6)      -> 6 keycode slots
    0x75, 0x08,        //   Report Size (8)
    0x15, 0x00,        //   Logical Minimum (0)
    0x25, 0x65,        //   Logical Maximum (101)
    0x05, 0x07,        //   Usage Page (Keyboard/Keypad)
    0x19, 0x00,        //   Usage Minimum (0)
    0x29, 0x65,        //   Usage Maximum (101)
    0x81, 0x00,        //   Input (Data,Array)
    0xC0,              // End Collection

    // --- Mouse, Report ID 3 ---
    0x05, 0x01,        // Usage Page (Generic Desktop)
    0x09, 0x02,        // Usage (Mouse)
    0xA1, 0x01,        // Collection (Application)
    0x09, 0x01,        //   Usage (Pointer)
    0xA1, 0x00,        //   Collection (Physical)
    0x85, 0x03,        //     Report ID (3)
    0x05, 0x09,        //     Usage Page (Button)
    0x19, 0x01,        //     Usage Minimum (Button 1)
    0x29, 0x05,        //     Usage Maximum (Button 5)
    0x15, 0x00,        //     Logical Minimum (0)
    0x25, 0x01,        //     Logical Maximum (1)
    0x75, 0x01,        //     Report Size (1)
    0x95, 0x05,        //     Report Count (5)    -> button bits
    0x81, 0x02,        //     Input (Data,Var,Abs)
    0x75, 0x03,        //     Report Size (3)
    0x95, 0x01,        //     Report Count (1)    -> padding
    0x81, 0x03,        //     Input (Const)
    0x05, 0x01,        //     Usage Page (Generic Desktop)
    0x09, 0x30,        //     Usage (X)
    0x09, 0x31,        //     Usage (Y)
    0x09, 0x38,        //     Usage (Wheel)
    0x15, 0x81,        //     Logical Minimum (-127)
    0x25, 0x7F,        //     Logical Maximum (127)
    0x75, 0x08,        //     Report Size (8)
    0x95, 0x03,        //     Report Count (3)    -> X, Y, wheel
    0x81, 0x06,        //     Input (Data,Var,Rel)
    0x05, 0x0C,        //     Usage Page (Consumer)
    0x0A, 0x38, 0x02,  //     Usage (AC Pan)
    0x15, 0x81,        //     Logical Minimum (-127)
    0x25, 0x7F,        //     Logical Maximum (127)
    0x75, 0x08,        //     Report Size (8)
    0x95, 0x01,        //     Report Count (1)    -> horizontal pan
    0x81, 0x06,        //     Input (Data,Var,Rel)
    0xC0,              //   End Collection
    0xC0,              // End Collection
};
const size_t hid_report_map_len = sizeof(hid_report_map);

#define KEY_SLOTS 6
#define USAGE_MOD_FIRST 0xE0
#define USAGE_MOD_LAST 0xE7

static esp_hidd_dev_t *s_dev;
static SemaphoreHandle_t s_mutex;

// Keyboard report: modifiers, reserved, keys[6].
static uint8_t s_modifiers;
static uint8_t s_keys[KEY_SLOTS];
// Mouse button state persists across MOVE/WHEEL reports.
static uint8_t s_buttons;

void hid_reports_init(esp_hidd_dev_t *dev)
{
    s_dev = dev;
    s_mutex = xSemaphoreCreateMutex();
}

static bool connected(void)
{
    return s_dev != NULL && esp_hidd_dev_connected(s_dev);
}

static esp_err_t send_keyboard_locked(void)
{
    uint8_t report[8] = { s_modifiers, 0 };
    memcpy(&report[2], s_keys, KEY_SLOTS);
    return esp_hidd_dev_input_set(s_dev, 0, HID_REPORT_ID_KEYBOARD, report,
                                  sizeof(report));
}

static esp_err_t send_mouse_locked(int8_t dx, int8_t dy, int8_t wheel, int8_t pan)
{
    uint8_t report[5] = { s_buttons, (uint8_t)dx, (uint8_t)dy, (uint8_t)wheel,
                          (uint8_t)pan };
    return esp_hidd_dev_input_set(s_dev, 0, HID_REPORT_ID_MOUSE, report,
                                  sizeof(report));
}

static int8_t clamp_step(int32_t v)
{
    if (v > 127) {
        return 127;
    }
    if (v < -127) {
        return -127;
    }
    return (int8_t)v;
}

esp_err_t hid_move(int16_t dx, int16_t dy)
{
    if (!connected()) {
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = ESP_OK;
    int32_t rx = dx;
    int32_t ry = dy;
    xSemaphoreTake(s_mutex, portMAX_DELAY);
    while ((rx != 0 || ry != 0) && err == ESP_OK) {
        int8_t sx = clamp_step(rx);
        int8_t sy = clamp_step(ry);
        err = send_mouse_locked(sx, sy, 0, 0);
        rx -= sx;
        ry -= sy;
    }
    xSemaphoreGive(s_mutex);
    return err;
}

esp_err_t hid_buttons(uint8_t mask)
{
    if (!connected()) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_mutex, portMAX_DELAY);
    esp_err_t err = ESP_OK;
    if (mask != s_buttons) {
        s_buttons = mask & 0x1F;
        err = send_mouse_locked(0, 0, 0, 0);
    }
    xSemaphoreGive(s_mutex);
    return err;
}

esp_err_t hid_wheel(int8_t vertical, int8_t horizontal)
{
    if (!connected()) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_mutex, portMAX_DELAY);
    esp_err_t err = send_mouse_locked(0, 0, vertical, horizontal);
    xSemaphoreGive(s_mutex);
    return err;
}

esp_err_t hid_key_down(uint8_t usage)
{
    if (!connected()) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_mutex, portMAX_DELAY);
    if (usage >= USAGE_MOD_FIRST && usage <= USAGE_MOD_LAST) {
        s_modifiers |= 1 << (usage - USAGE_MOD_FIRST);
    } else {
        bool present = false;
        int free_slot = -1;
        for (int i = 0; i < KEY_SLOTS; i++) {
            if (s_keys[i] == usage) {
                present = true;
            } else if (s_keys[i] == 0 && free_slot < 0) {
                free_slot = i;
            }
        }
        if (!present) {
            if (free_slot < 0) {
                // 6KRO rollover exceeded: drop the key, keep the report valid.
                xSemaphoreGive(s_mutex);
                return ESP_OK;
            }
            s_keys[free_slot] = usage;
        }
    }
    esp_err_t err = send_keyboard_locked();
    xSemaphoreGive(s_mutex);
    return err;
}

esp_err_t hid_key_up(uint8_t usage)
{
    if (!connected()) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_mutex, portMAX_DELAY);
    if (usage >= USAGE_MOD_FIRST && usage <= USAGE_MOD_LAST) {
        s_modifiers &= ~(1 << (usage - USAGE_MOD_FIRST));
    } else {
        for (int i = 0; i < KEY_SLOTS; i++) {
            if (s_keys[i] == usage) {
                s_keys[i] = 0;
            }
        }
    }
    esp_err_t err = send_keyboard_locked();
    xSemaphoreGive(s_mutex);
    return err;
}

esp_err_t hid_release_all(void)
{
    if (!connected()) {
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreTake(s_mutex, portMAX_DELAY);
    s_modifiers = 0;
    memset(s_keys, 0, sizeof(s_keys));
    s_buttons = 0;
    esp_err_t err = send_keyboard_locked();
    esp_err_t err2 = send_mouse_locked(0, 0, 0, 0);
    xSemaphoreGive(s_mutex);
    return err != ESP_OK ? err : err2;
}
