#include "connection_led.h"

#include <Arduino.h>
#include <BleCombo.h>

namespace bridge {
namespace {

#ifdef LED_BUILTIN
constexpr uint8_t kBuiltinLedPin = LED_BUILTIN;
#else
constexpr uint8_t kBuiltinLedPin = 2;
#endif

constexpr uint32_t kConnectedLedPulseMs = 200;
constexpr uint32_t kConnectedLedIntervalMs = 20000;

bool gWasConnected = false;
bool gPulseActive = false;
uint32_t gLastPulseStartMs = 0;

void setBuiltinLed(bool on) {
  digitalWrite(kBuiltinLedPin, on ? HIGH : LOW);
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
