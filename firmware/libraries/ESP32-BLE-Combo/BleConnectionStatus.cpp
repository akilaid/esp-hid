#include "BleConnectionStatus.h"

BleConnectionStatus::BleConnectionStatus(void) {
}

void BleConnectionStatus::onConnect(NimBLEServer* pServer, NimBLEConnInfo& connInfo)
{
  this->connected = true;

  // Request a fast connection interval (7.5-15ms) so mouse/keyboard HID
  // reports are delivered with low latency and movement feels smooth.
  // Units: interval in 1.25ms steps, supervision timeout in 10ms steps.
  pServer->updateConnParams(connInfo.getConnHandle(), 6, 12, 0, 200);
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
