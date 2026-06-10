#include <Arduino.h>
#include "libraries/ESP32-BLE-Combo/BleCombo.h"
#include "bridge_types.h"
#include "connection_led.h"
#include "serial_processor.h"

void setup() {
  bridge::initConnectionLed();

  Serial.begin(bridge::kSerialBaud);
  Keyboard.deviceName = "ESP-HID-ME";
  Keyboard.deviceManufacturer = "AKILA INDUNIL";
  Keyboard.begin();
  Mouse.begin();
}

void loop() {
  bridge::updateConnectionLed();
  bridge::processSerial();
  delay(0);
}