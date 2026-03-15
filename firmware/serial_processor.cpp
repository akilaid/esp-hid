#include "serial_processor.h"

#include <Arduino.h>

#include "bridge_types.h"
#include "command_dispatch.h"

namespace bridge {
namespace {

SerialLineState serialLineState;

void resetSerialLineState() {
  serialLineState.length = 0;
  serialLineState.overflow = false;
}

void handleCompletedLine(char* rawLine) {
  CommandFrame frame;
  if (!splitCommandLine(rawLine, &frame)) {
    return;
  }

  dispatchCommand(frame);
}

void processSerialByte(char c) {
  if (serialLineState.overflow) {
    if (c == '\n') {
      resetSerialLineState();
    }
    return;
  }

  if (c == '\r') {
    return;
  }

  if (c == '\n') {
    serialLineState.buffer[serialLineState.length] = '\0';
    handleCompletedLine(serialLineState.buffer);
    serialLineState.length = 0;
    return;
  }

  if (serialLineState.length < (kMaxLineLength - 1)) {
    serialLineState.buffer[serialLineState.length++] = c;
    return;
  }

  // Drop oversized lines until newline so malformed frames do not desync parsing.
  serialLineState.overflow = true;
  serialLineState.length = 0;
}

}  // namespace

void processSerial() {
  while (Serial.available() > 0) {
    processSerialByte(static_cast<char>(Serial.read()));
  }
}

}  // namespace bridge