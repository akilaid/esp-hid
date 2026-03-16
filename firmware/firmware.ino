#include <Arduino.h>
#include <BleCombo.h>
#include "bridge_types.h"
#include "connection_led.h"
#include "serial_processor.h"

void setup() {
  bridge::initConnectionLed();

  Serial.begin(bridge::kSerialBaud);
  Keyboard.deviceName = "PC Bridge Combo";
  Keyboard.deviceManufacturer = "ESP HID Bridge";
  Keyboard.begin();
  Mouse.begin();
}

void loop() {
  bridge::updateConnectionLed();
  bridge::processSerial();
  delay(0);
}