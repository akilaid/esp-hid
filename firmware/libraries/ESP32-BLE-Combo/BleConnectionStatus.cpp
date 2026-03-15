#include "BleConnectionStatus.h"

namespace {

// BLE units: interval=1.25ms, timeout=10ms.
constexpr uint16_t kPreferredConnMinInterval = 6;   // 7.5ms
constexpr uint16_t kPreferredConnMaxInterval = 12;  // 15ms
constexpr uint16_t kPreferredConnLatency = 0;
constexpr uint16_t kPreferredConnTimeout = 200;     // 2s

}  // namespace

BleConnectionStatus::BleConnectionStatus(void) {
}

void BleConnectionStatus::onConnect(BLEServer* pServer)
{
  this->connected = true;

  BLE2902* desc = (BLE2902*)this->inputKeyboard->getDescriptorByUUID(BLEUUID((uint16_t)0x2902));
  desc->setNotifications(true);
  desc = (BLE2902*)this->inputMouse->getDescriptorByUUID(BLEUUID((uint16_t)0x2902));
  desc->setNotifications(true);
}

#if defined(CONFIG_BLUEDROID_ENABLED)
void BleConnectionStatus::onConnect(BLEServer* pServer, esp_ble_gatts_cb_param_t* param)
{
  if (pServer == nullptr || param == nullptr) {
    return;
  }

  pServer->requestConnParams(param->connect.remote_bda,
                             kPreferredConnMinInterval,
                             kPreferredConnMaxInterval,
                             kPreferredConnLatency,
                             kPreferredConnTimeout);
}
#endif

void BleConnectionStatus::onDisconnect(BLEServer* pServer)
{
  this->connected = false;
  BLE2902* desc = (BLE2902*)this->inputKeyboard->getDescriptorByUUID(BLEUUID((uint16_t)0x2902));
  desc->setNotifications(false);
  desc = (BLE2902*)this->inputMouse->getDescriptorByUUID(BLEUUID((uint16_t)0x2902));
  desc->setNotifications(false);  
}

#if defined(CONFIG_BLUEDROID_ENABLED)
void BleConnectionStatus::onDisconnect(BLEServer* pServer, esp_ble_gatts_cb_param_t* param)
{
  (void)pServer;
  (void)param;
}
#endif
