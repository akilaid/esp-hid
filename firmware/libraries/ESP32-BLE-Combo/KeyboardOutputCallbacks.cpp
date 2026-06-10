#include "KeyboardOutputCallbacks.h"

KeyboardOutputCallbacks::KeyboardOutputCallbacks(void) {
}

void KeyboardOutputCallbacks::onWrite(NimBLECharacteristic* me, NimBLEConnInfo& connInfo) {
  // Host writes the keyboard LED state (caps/num/scroll lock) here. Unused.
  (void)me;
  (void)connInfo;
}
