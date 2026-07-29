// Status LED: solid = no BLE client (advertising/idle); a 200 ms pulse every
// 20 s = connected. Same observable behavior as the legacy firmware.
#pragma once

#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

void status_led_init(void);
void status_led_set_connected(bool connected);

#ifdef __cplusplus
}
#endif
