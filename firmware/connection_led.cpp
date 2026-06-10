#include "connection_led.h"

#include <Arduino.h>
#include "libraries/ESP32-BLE-Combo/BleCombo.h"

namespace bridge {
namespace {

// Built-in LED wiring varies by board:
//   - ESP32-C3 Super Mini: LED on GPIO8, active-LOW (drive LOW to turn on).
//   - Classic ESP32 (WROOM) dev boards: LED on GPIO2, active-HIGH.
// Define BRIDGE_LED_PIN / BRIDGE_LED_ACTIVE_LOW at build time to override.
#if !defined(BRIDGE_LED_PIN)
  #if defined(CONFIG_IDF_TARGET_ESP32C3)
    #define BRIDGE_LED_PIN 8
    #define BRIDGE_LED_ACTIVE_LOW 1
  #elif defined(LED_BUILTIN)
    #define BRIDGE_LED_PIN LED_BUILTIN
    #define BRIDGE_LED_ACTIVE_LOW 0
  #else
    #define BRIDGE_LED_PIN 2
    #define BRIDGE_LED_ACTIVE_LOW 0
  #endif
#endif

#if !defined(BRIDGE_LED_ACTIVE_LOW)
  #define BRIDGE_LED_ACTIVE_LOW 0
#endif

constexpr uint8_t kBuiltinLedPin = BRIDGE_LED_PIN;
constexpr bool kBuiltinLedActiveLow = BRIDGE_LED_ACTIVE_LOW;

constexpr uint32_t kConnectedLedPulseMs = 200;
constexpr uint32_t kConnectedLedIntervalMs = 20000;

bool gWasConnected = false;
bool gPulseActive = false;
uint32_t gLastPulseStartMs = 0;

void setBuiltinLed(bool on) {
  const bool level = kBuiltinLedActiveLow ? !on : on;
  digitalWrite(kBuiltinLedPin, level ? HIGH : LOW);
}

}  // namespace

void initConnectionLed() {
  pinMode(kBuiltinLedPin, OUTPUT);
  setBuiltinLed(true);

  gWasConnected = false;
  gPulseActive = false;
  gLastPulseStartMs = 0;
}

void updateConnectionLed() {
  const bool connected = Keyboard.isConnected();
  const uint32_t nowMs = millis();

  if (!connected) {
    // Keep the built-in LED on continuously until a device connects.
    setBuiltinLed(true);
    gWasConnected = false;
    gPulseActive = false;
    return;
  }

  if (!gWasConnected) {
    // On initial connect, emit a pulse immediately.
    gWasConnected = true;
    gPulseActive = true;
    gLastPulseStartMs = nowMs;
    setBuiltinLed(true);
    return;
  }

  if (gPulseActive) {
    if ((nowMs - gLastPulseStartMs) >= kConnectedLedPulseMs) {
      gPulseActive = false;
      setBuiltinLed(false);
    }
    return;
  }

  if ((nowMs - gLastPulseStartMs) >= kConnectedLedIntervalMs) {
    gPulseActive = true;
    gLastPulseStartMs = nowMs;
    setBuiltinLed(true);
  }
}

}  // namespace bridge
