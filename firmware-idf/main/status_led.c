#include "status_led.h"

#include "driver/gpio.h"
#include "esp_timer.h"
#include "sdkconfig.h"

#define LED_GPIO CONFIG_BRIDGE_LED_GPIO

// Connected pattern: 200 ms on, then off until 20 s after the pulse started.
#define PULSE_ON_US (200 * 1000)
#define PULSE_PERIOD_US (20 * 1000 * 1000)

static esp_timer_handle_t s_timer;
static volatile bool s_connected;

static void led_write(bool on)
{
#if CONFIG_BRIDGE_LED_ACTIVE_LOW
    gpio_set_level(LED_GPIO, on ? 0 : 1);
#else
    gpio_set_level(LED_GPIO, on ? 1 : 0);
#endif
}

static void pulse_off_cb(void *arg)
{
    (void)arg;
    if (s_connected) {
        led_write(false);
    }
}

static void pulse_start_cb(void *arg);

static esp_timer_handle_t s_off_timer;

static void pulse_start_cb(void *arg)
{
    (void)arg;
    if (!s_connected) {
        return;
    }
    led_write(true);
    esp_timer_start_once(s_off_timer, PULSE_ON_US);
}

void status_led_init(void)
{
    gpio_config_t cfg = {
        .pin_bit_mask = 1ULL << LED_GPIO,
        .mode = GPIO_MODE_OUTPUT,
    };
    gpio_config(&cfg);
    // Lit from the very first moment, matching the legacy firmware: solid
    // means "no BLE client yet".
    led_write(true);

    const esp_timer_create_args_t pulse_args = {
        .callback = pulse_start_cb,
        .name = "led_pulse",
    };
    esp_timer_create(&pulse_args, &s_timer);
    const esp_timer_create_args_t off_args = {
        .callback = pulse_off_cb,
        .name = "led_off",
    };
    esp_timer_create(&off_args, &s_off_timer);
}

void status_led_set_connected(bool connected)
{
    if (connected == s_connected) {
        return;
    }
    s_connected = connected;
    if (connected) {
        // Acknowledge the connection with an immediate pulse, then heartbeat.
        led_write(true);
        esp_timer_start_once(s_off_timer, PULSE_ON_US);
        esp_timer_start_periodic(s_timer, PULSE_PERIOD_US);
    } else {
        esp_timer_stop(s_timer);
        esp_timer_stop(s_off_timer);
        led_write(true); // solid: waiting for a client
    }
}
