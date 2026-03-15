#include <Arduino.h>
#include <BleCombo.h>

namespace {
constexpr uint32_t kSerialBaud = 115200;
constexpr size_t kMaxLineLength = 96;
constexpr int kMaxHidDelta = 127;
constexpr int kMinHidDelta = -127;

char lineBuffer[kMaxLineLength];
size_t lineLength = 0;
bool lineOverflow = false;

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

bool parseKeyCode(const char* args, uint8_t* keyCodeOut) {
  if (args == nullptr || keyCodeOut == nullptr) {
    return false;
  }

  long keyCode = 0;
  char trailing = '\0';
  if (sscanf(args, "%ld %c", &keyCode, &trailing) != 1) {
    return false;
  }

  if (keyCode < 0 || keyCode > 255) {
    return false;
  }

  *keyCodeOut = static_cast<uint8_t>(keyCode);
  return true;
}

void handleMoveCommand(const char* args) {
  long dx = 0;
  long dy = 0;
  char trailing = '\0';
  if (sscanf(args, "%ld %ld %c", &dx, &dy, &trailing) != 2) {
    return;
  }

  if (!hidConnected()) {
    return;
  }

  if (dx == 0 && dy == 0) {
    return;
  }

  sendChunkedMove(dx, dy, 0);
}

void handleClickCommand(const char* args) {
  if (!hidConnected()) {
    return;
  }

  if (strcmp(args, "LEFT") == 0) {
    Mouse.click(MOUSE_LEFT);
    return;
  }

  if (strcmp(args, "RIGHT") == 0) {
    Mouse.click(MOUSE_RIGHT);
    return;
  }
}

void handleScrollCommand(const char* args) {
  long amount = 0;
  char trailing = '\0';
  if (sscanf(args, "%ld %c", &amount, &trailing) != 1) {
    return;
  }

  if (!hidConnected() || amount == 0) {
    return;
  }

  sendChunkedMove(0, 0, amount);
}

void handleKeyDownCommand(const char* args) {
  if (!hidConnected()) {
    return;
  }

  uint8_t keyCode = 0;
  if (!parseKeyCode(args, &keyCode)) {
    return;
  }

  Keyboard.press(keyCode);
}

void handleKeyUpCommand(const char* args) {
  if (!hidConnected()) {
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

void handleLine(char* rawLine) {
  char* line = trimWhitespace(rawLine);
  if (*line == '\0') {
    return;
  }

  char* separator = strchr(line, ' ');
  char* command = line;
  char* args = nullptr;

  if (separator != nullptr) {
    *separator = '\0';
    args = trimWhitespace(separator + 1);
  }

  if (strcmp(command, "MOVE") == 0 && args != nullptr) {
    handleMoveCommand(args);
    return;
  }

  if (strcmp(command, "CLICK") == 0 && args != nullptr) {
    handleClickCommand(args);
    return;
  }

  if (strcmp(command, "SCROLL") == 0 && args != nullptr) {
    handleScrollCommand(args);
    return;
  }

  if (strcmp(command, "KEYDOWN") == 0 && args != nullptr) {
    handleKeyDownCommand(args);
    return;
  }

  if (strcmp(command, "KEYUP") == 0 && args != nullptr) {
    handleKeyUpCommand(args);
    return;
  }

  if (strcmp(command, "KEYRELEASE") == 0) {
    if (hidConnected()) {
      Keyboard.releaseAll();
    }
    return;
  }

  if (strcmp(command, "RELEASE") == 0) {
    releaseMouseButtons();
  }
}

void processSerial() {
  while (Serial.available() > 0) {
    const char c = static_cast<char>(Serial.read());

    if (lineOverflow) {
      if (c == '\n') {
        lineOverflow = false;
        lineLength = 0;
      }
      continue;
    }

    if (c == '\r') {
      continue;
    }

    if (c == '\n') {
      lineBuffer[lineLength] = '\0';
      handleLine(lineBuffer);
      lineLength = 0;
      continue;
    }

    if (lineLength < (kMaxLineLength - 1)) {
      lineBuffer[lineLength++] = c;
    } else {
      // Drop oversized lines until newline so malformed frames do not desync parsing.
      lineOverflow = true;
      lineLength = 0;
    }
  }
}
}  // namespace

void setup() {
  Serial.begin(kSerialBaud);
  Keyboard.deviceName = "PC Bridge Combo";
  Keyboard.deviceManufacturer = "ESP HID Bridge";
  Keyboard.begin();
  Mouse.begin();
}

void loop() {
  processSerial();
  delay(0);
}