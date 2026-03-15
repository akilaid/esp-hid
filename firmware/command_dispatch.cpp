#include "command_dispatch.h"

#include <BleCombo.h>

namespace bridge {
namespace {

constexpr const char* kCmdMove = "MOVE";
constexpr const char* kCmdClick = "CLICK";
constexpr const char* kCmdMouseDown = "MOUSEDOWN";
constexpr const char* kCmdMouseUp = "MOUSEUP";
constexpr const char* kCmdScroll = "SCROLL";
constexpr const char* kCmdKeyDown = "KEYDOWN";
constexpr const char* kCmdKeyUp = "KEYUP";
constexpr const char* kCmdKeyRelease = "KEYRELEASE";
constexpr const char* kCmdRelease = "RELEASE";

constexpr const char* kClickLeft = "LEFT";
constexpr const char* kClickRight = "RIGHT";
constexpr const char* kClickMiddle = "MIDDLE";

char* trimWhitespace(char* text) {
  while (*text != '\0' && isspace(static_cast<unsigned char>(*text))) {
    ++text;
  }

  if (*text == '\0') {
    return text;
  }

  char* end = text + strlen(text) - 1;
  while (end > text && isspace(static_cast<unsigned char>(*end))) {
    *end = '\0';
    --end;
  }

  return text;
}

bool hidConnected() {
  return Keyboard.isConnected();
}

int clampToHidRange(long value) {
  if (value > kMaxHidDelta) {
    return kMaxHidDelta;
  }
  if (value < kMinHidDelta) {
    return kMinHidDelta;
  }
  return static_cast<int>(value);
}

void sendChunkedMove(long dx, long dy, long wheel) {
  // HID mouse reports are int8 deltas, so split larger values into chunks.
  while (dx != 0 || dy != 0 || wheel != 0) {
    int stepX = clampToHidRange(dx);
    int stepY = clampToHidRange(dy);
    int stepWheel = clampToHidRange(wheel);

    Mouse.move(static_cast<int8_t>(stepX), static_cast<int8_t>(stepY),
               static_cast<int8_t>(stepWheel));

    dx -= stepX;
    dy -= stepY;
    wheel -= stepWheel;
  }
}

bool parseMoveArgs(const char* args, long* dxOut, long* dyOut) {
  if (args == nullptr || dxOut == nullptr || dyOut == nullptr) {
    return false;
  }

  char trailing = '\0';
  return sscanf(args, "%ld %ld %c", dxOut, dyOut, &trailing) == 2;
}

bool parseSingleLongArg(const char* args, long* valueOut) {
  if (args == nullptr || valueOut == nullptr) {
    return false;
  }

  char trailing = '\0';
  return sscanf(args, "%ld %c", valueOut, &trailing) == 1;
}

bool parseKeyCode(const char* args, uint8_t* keyCodeOut) {
  if (keyCodeOut == nullptr) {
    return false;
  }

  long keyCode = 0;
  if (!parseSingleLongArg(args, &keyCode)) {
    return false;
  }

  if (keyCode < 0 || keyCode > 255) {
    return false;
  }

  *keyCodeOut = static_cast<uint8_t>(keyCode);
  return true;
}

bool parseMouseButton(const char* args, uint8_t* buttonOut) {
  if (args == nullptr || buttonOut == nullptr) {
    return false;
  }

  if (strcmp(args, kClickLeft) == 0) {
    *buttonOut = MOUSE_LEFT;
    return true;
  }

  if (strcmp(args, kClickRight) == 0) {
    *buttonOut = MOUSE_RIGHT;
    return true;
  }

  if (strcmp(args, kClickMiddle) == 0) {
    *buttonOut = MOUSE_MIDDLE;
    return true;
  }

  return false;
}

void executeMove(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  long dx = 0;
  long dy = 0;
  if (!parseMoveArgs(args, &dx, &dy)) {
    return;
  }

  if (dx == 0 && dy == 0) {
    return;
  }

  sendChunkedMove(dx, dy, 0);
}

void executeClick(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  if (strcmp(args, kClickLeft) == 0) {
    Mouse.click(MOUSE_LEFT);
    return;
  }

  if (strcmp(args, kClickRight) == 0) {
    Mouse.click(MOUSE_RIGHT);
    return;
  }

  if (strcmp(args, kClickMiddle) == 0) {
    Mouse.click(MOUSE_MIDDLE);
  }
}

void executeMouseDown(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  uint8_t button = 0;
  if (!parseMouseButton(args, &button)) {
    return;
  }

  Mouse.press(button);
}

void executeMouseUp(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  uint8_t button = 0;
  if (!parseMouseButton(args, &button)) {
    return;
  }

  Mouse.release(button);
}

void executeScroll(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  long amount = 0;
  if (!parseSingleLongArg(args, &amount)) {
    return;
  }

  if (amount == 0) {
    return;
  }

  sendChunkedMove(0, 0, amount);
}

void executeKeyDown(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  uint8_t keyCode = 0;
  if (!parseKeyCode(args, &keyCode)) {
    return;
  }

  Keyboard.press(keyCode);
}

void executeKeyUp(const char* args) {
  if (!hidConnected() || args == nullptr) {
    return;
  }

  uint8_t keyCode = 0;
  if (!parseKeyCode(args, &keyCode)) {
    return;
  }

  Keyboard.release(keyCode);
}

void releaseMouseButtons() {
  if (!hidConnected()) {
    return;
  }

  Mouse.release(MOUSE_LEFT);
  Mouse.release(MOUSE_RIGHT);
  Mouse.release(MOUSE_MIDDLE);
}

void releaseKeyboardKeys() {
  if (!hidConnected()) {
    return;
  }

  Keyboard.releaseAll();
}

}  // namespace

bool splitCommandLine(char* rawLine, CommandFrame* frameOut) {
  if (rawLine == nullptr || frameOut == nullptr) {
    return false;
  }

  char* line = trimWhitespace(rawLine);
  if (*line == '\0') {
    return false;
  }

  char* separator = strchr(line, ' ');
  frameOut->command = line;
  frameOut->args = nullptr;

  if (separator != nullptr) {
    *separator = '\0';
    frameOut->args = trimWhitespace(separator + 1);
  }

  return true;
}

void dispatchCommand(const CommandFrame& frame) {
  if (frame.command == nullptr) {
    return;
  }

  if (strcmp(frame.command, kCmdMove) == 0) {
    executeMove(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdClick) == 0) {
    executeClick(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdMouseDown) == 0) {
    executeMouseDown(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdMouseUp) == 0) {
    executeMouseUp(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdScroll) == 0) {
    executeScroll(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdKeyDown) == 0) {
    executeKeyDown(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdKeyUp) == 0) {
    executeKeyUp(frame.args);
    return;
  }

  if (strcmp(frame.command, kCmdKeyRelease) == 0) {
    releaseKeyboardKeys();
    return;
  }

  if (strcmp(frame.command, kCmdRelease) == 0) {
    releaseMouseButtons();
  }
}

}  // namespace bridge