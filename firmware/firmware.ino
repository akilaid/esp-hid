#include <Arduino.h>
#include <BleCombo.h>
#include "bridge_types.h"
#include "serial_processor.h"

void setup() {
  Serial.begin(bridge::kSerialBaud);
  Keyboard.deviceName = "PC Bridge Combo";
  Keyboard.deviceManufacturer = "ESP HID Bridge";
  Keyboard.begin();
  Mouse.begin();
}

void loop() {
  bridge::processSerial();
  delay(0);
}