// BLE HID device: esp_hidd over NimBLE, plus GAP advertising, connection
// parameter tuning, and bond management.
#pragma once

#include <stdint.h>

#include "esp_err.h"
#include "esp_hidd.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef enum {
    BLE_HID_IDLE = 0,        // stack down / not advertising
    BLE_HID_ADVERTISING = 1,
    BLE_HID_CONNECTED = 2,   // HID host subscribed and ready
} ble_hid_state_t;

typedef struct {
    // Called on every state change. reason is the BLE disconnect reason of
    // the most recent disconnect (0 if none). May be invoked from the NimBLE
    // host task or the esp_hidd event loop.
    void (*on_state)(ble_hid_state_t state, uint8_t reason);
} ble_hid_callbacks_t;

esp_err_t ble_hid_init(const ble_hid_callbacks_t *callbacks);

esp_hidd_dev_t *ble_hid_dev(void);
ble_hid_state_t ble_hid_state(void);
uint8_t ble_hid_bond_count(void);
uint8_t ble_hid_last_disconnect_reason(void);

// Deletes all stored bonds. Terminates an active connection first so the
// peer re-pairs cleanly.
esp_err_t ble_hid_clear_bonds(void);

#ifdef __cplusplus
}
#endif
