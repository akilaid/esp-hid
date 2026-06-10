#include "BleConnectionStatus.h"

BleConnectionStatus::BleConnectionStatus(void) {
}

void BleConnectionStatus::onConnect(NimBLEServer* pServer, NimBLEConnInfo& connInfo)
{
  (void)pServer;
  (void)connInfo;
  this->connected = true;
}

void BleConnectionStatus::onDisconnect(NimBLEServer* pServer, NimBLEConnInfo& connInfo, int reason)
{
  (void)pServer;
  (void)connInfo;
  (void)reason;
  this->connected = false;

  // Resume advertising so the host can reconnect after a drop.
  NimBLEDevice::startAdvertising();
}
