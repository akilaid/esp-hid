// Binary wire protocol codec — see docs/PROTOCOL.md.
// Pure C, no OS dependencies: unit-testable off-target with plain cc.
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define PROTO_SYNC1 0xAA
#define PROTO_SYNC2 0x55
#define PROTO_MAX_PAYLOAD 32
// sync(2) + type(1) + len(1) + payload + crc(1)
#define PROTO_MAX_FRAME (2 + 1 + 1 + PROTO_MAX_PAYLOAD + 1)
#define PROTO_VERSION 1

// Host -> device
#define PROTO_PING 0x01
#define PROTO_GET_STATUS 0x02
#define PROTO_MOVE 0x10
#define PROTO_BUTTONS 0x11
#define PROTO_WHEEL 0x12
#define PROTO_KEY_DOWN 0x13
#define PROTO_KEY_UP 0x14
#define PROTO_RELEASE_ALL 0x15
#define PROTO_CLEAR_BONDS 0x20

// Device -> host
#define PROTO_HELLO 0x81
#define PROTO_BLE_STATE 0x82
#define PROTO_ACK 0x83
#define PROTO_ERROR 0x84
#define PROTO_PONG 0x85
#define PROTO_LOG 0x86

// ERROR codes
#define PROTO_ERR_BAD_CRC 1
#define PROTO_ERR_UNKNOWN_TYPE 2
#define PROTO_ERR_BAD_LEN 3
#define PROTO_ERR_HID_SEND_FAIL 4
#define PROTO_ERR_NOT_CONNECTED_DROP 5

typedef struct {
    uint8_t type;
    uint8_t len;
    uint8_t payload[PROTO_MAX_PAYLOAD];
} proto_frame_t;

typedef enum {
    PROTO_DECODE_OK = 0,       // no complete frame yet, no error
    PROTO_DECODE_FRAME,        // *out holds a complete valid frame
    PROTO_DECODE_ERR_BAD_LEN,  // frame rejected: len > PROTO_MAX_PAYLOAD
    PROTO_DECODE_ERR_BAD_CRC,  // frame rejected: checksum mismatch
} proto_decode_result_t;

typedef struct {
    uint8_t state;
    proto_frame_t frame;
    uint8_t got;
} proto_decoder_t;

// CRC-8 poly 0x07, init 0x00, no reflection, no final XOR.
uint8_t proto_crc8(const uint8_t *data, size_t len);

// Serializes a frame into out (at least PROTO_MAX_FRAME bytes).
// Returns the encoded size, or 0 if len > PROTO_MAX_PAYLOAD.
size_t proto_encode(uint8_t type, const void *payload, uint8_t len,
                    uint8_t out[PROTO_MAX_FRAME]);

// Feeds one byte. On PROTO_DECODE_FRAME, *out is valid. On error results the
// decoder has already reset and will resync on the next 0xAA 0x55.
proto_decode_result_t proto_decoder_feed(proto_decoder_t *dec, uint8_t byte,
                                         proto_frame_t *out);

void proto_decoder_reset(proto_decoder_t *dec);

#ifdef __cplusplus
}
#endif
