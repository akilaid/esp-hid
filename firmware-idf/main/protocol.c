#include "protocol.h"

#include <string.h>

enum {
    ST_WAIT_AA,
    ST_WAIT_55,
    ST_TYPE,
    ST_LEN,
    ST_PAYLOAD,
    ST_CRC,
};

uint8_t proto_crc8(const uint8_t *data, size_t len)
{
    uint8_t crc = 0;
    for (size_t i = 0; i < len; i++) {
        crc ^= data[i];
        for (int bit = 0; bit < 8; bit++) {
            crc = (crc & 0x80) ? (uint8_t)((crc << 1) ^ 0x07) : (uint8_t)(crc << 1);
        }
    }
    return crc;
}

size_t proto_encode(uint8_t type, const void *payload, uint8_t len,
                    uint8_t out[PROTO_MAX_FRAME])
{
    if (len > PROTO_MAX_PAYLOAD) {
        return 0;
    }
    out[0] = PROTO_SYNC1;
    out[1] = PROTO_SYNC2;
    out[2] = type;
    out[3] = len;
    if (len > 0) {
        memcpy(&out[4], payload, len);
    }
    out[4 + len] = proto_crc8(&out[2], (size_t)len + 2);
    return (size_t)len + 5;
}

void proto_decoder_reset(proto_decoder_t *dec)
{
    dec->state = ST_WAIT_AA;
    dec->got = 0;
}

proto_decode_result_t proto_decoder_feed(proto_decoder_t *dec, uint8_t byte,
                                         proto_frame_t *out)
{
    switch (dec->state) {
    case ST_WAIT_AA:
        if (byte == PROTO_SYNC1) {
            dec->state = ST_WAIT_55;
        }
        break;
    case ST_WAIT_55:
        if (byte == PROTO_SYNC2) {
            dec->state = ST_TYPE;
        } else if (byte != PROTO_SYNC1) {
            // A run of 0xAA stays here: the last one may start a frame.
            dec->state = ST_WAIT_AA;
        }
        break;
    case ST_TYPE:
        dec->frame.type = byte;
        dec->state = ST_LEN;
        break;
    case ST_LEN:
        if (byte > PROTO_MAX_PAYLOAD) {
            proto_decoder_reset(dec);
            return PROTO_DECODE_ERR_BAD_LEN;
        }
        dec->frame.len = byte;
        dec->got = 0;
        dec->state = (byte == 0) ? ST_CRC : ST_PAYLOAD;
        break;
    case ST_PAYLOAD:
        dec->frame.payload[dec->got++] = byte;
        if (dec->got == dec->frame.len) {
            dec->state = ST_CRC;
        }
        break;
    case ST_CRC: {
        // CRC covers type | len | payload; those bytes are contiguous only
        // conceptually, so compute over a small scratch header + payload.
        uint8_t header[2] = { dec->frame.type, dec->frame.len };
        uint8_t crc = proto_crc8(header, 2);
        // Continue the CRC over the payload without re-copying: fold the
        // remaining bytes through the same bitwise loop.
        for (uint8_t i = 0; i < dec->frame.len; i++) {
            crc ^= dec->frame.payload[i];
            for (int bit = 0; bit < 8; bit++) {
                crc = (crc & 0x80) ? (uint8_t)((crc << 1) ^ 0x07) : (uint8_t)(crc << 1);
            }
        }
        proto_decoder_reset(dec);
        if (crc != byte) {
            return PROTO_DECODE_ERR_BAD_CRC;
        }
        *out = dec->frame;
        return PROTO_DECODE_FRAME;
    }
    default:
        proto_decoder_reset(dec);
        break;
    }
    return PROTO_DECODE_OK;
}
