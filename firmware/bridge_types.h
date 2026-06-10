#pragma once

#include <Arduino.h>

namespace bridge {

constexpr uint32_t kSerialBaud = 115200;
constexpr size_t kMaxLineLength = 96;
constexpr int kMaxHidDelta = 127;
constexpr int kMinHidDelta = -127;

struct SerialLineState {
  char buffer[kMaxLineLength];
  size_t length = 0;
  bool overflow = false;
};

struct CommandFrame {
  const char* command = nullptr;
  const char* args = nullptr;
};

}  // namespace bridge