// Off-target test for main/protocol.c against the PROTOCOL.md vectors.
// Build & run:  cc -Wall -Wextra -Werror -I../main tools/test_protocol.c main/protocol.c -o build_test && ./build_test
#include <stdio.h>
#include <string.h>

#include "protocol.h"

static int failures = 0;

#define CHECK(cond, ...)                                  \
    do {                                                  \
        if (!(cond)) {                                    \
            failures++;                                   \
            printf("FAIL %s:%d: ", __FILE__, __LINE__);   \
            printf(__VA_ARGS__);                          \
            printf("\n");                                 \
        }                                                 \
    } while (0)

// Spec CRC vectors: crc8 over type|len|payload.
static void test_crc_vectors(void)
{
    const struct {
        const uint8_t *data;
        size_t len;
        uint8_t crc;
    } vectors[] = {
        { (const uint8_t[]){ 0x01, 0x04, 0x78, 0x56, 0x34, 0x12 }, 6, 0xAE },
        { (const uint8_t[]){ 0x02, 0x00 }, 2, 0x2A },
        { (const uint8_t[]){ 0x10, 0x04, 0x05, 0x00, 0xFB, 0xFF }, 6, 0x2F },
        { (const uint8_t[]){ 0x15, 0x00 }, 2, 0x16 },
        { (const uint8_t[]){ 0x81, 0x06, 0x01, 0x01, 0x00, 0x00, 0x03, 0x00 }, 8, 0x14 },
    };
    for (size_t i = 0; i < sizeof(vectors) / sizeof(vectors[0]); i++) {
        uint8_t got = proto_crc8(vectors[i].data, vectors[i].len);
        CHECK(got == vectors[i].crc, "crc vector %zu: got %02X want %02X",
              i, got, vectors[i].crc);
    }
}

static void test_encode_ping(void)
{
    static const uint8_t want[] = { 0xAA, 0x55, 0x01, 0x04, 0x78, 0x56, 0x34, 0x12, 0xAE };
    uint8_t out[PROTO_MAX_FRAME];
    const uint8_t nonce[] = { 0x78, 0x56, 0x34, 0x12 };
    size_t n = proto_encode(PROTO_PING, nonce, 4, out);
    CHECK(n == sizeof(want), "encode size %zu want %zu", n, sizeof(want));
    CHECK(memcmp(out, want, sizeof(want)) == 0, "PING bytes mismatch");
}

static void test_decode_roundtrip(void)
{
    uint8_t out[PROTO_MAX_FRAME];
    const uint8_t move[] = { 0x05, 0x00, 0xFB, 0xFF };
    size_t n = proto_encode(PROTO_MOVE, move, 4, out);

    proto_decoder_t dec;
    proto_decoder_reset(&dec);
    proto_frame_t frame;
    int frames = 0;
    for (size_t i = 0; i < n; i++) {
        if (proto_decoder_feed(&dec, out[i], &frame) == PROTO_DECODE_FRAME) {
            frames++;
        }
    }
    CHECK(frames == 1, "decoded %d frames want 1", frames);
    CHECK(frame.type == PROTO_MOVE && frame.len == 4, "type/len mismatch");
    CHECK(memcmp(frame.payload, move, 4) == 0, "payload mismatch");
}

static void test_resync_after_garbage(void)
{
    uint8_t stream[128];
    size_t pos = 0;
    // noise + false sync start
    const uint8_t noise[] = { 0x00, 0xFF, 0xAA, 0x12 };
    memcpy(&stream[pos], noise, sizeof(noise));
    pos += sizeof(noise);
    // valid GET_STATUS
    pos += proto_encode(PROTO_GET_STATUS, NULL, 0, &stream[pos]);
    // truncated frame header that will swallow the next frame and fail CRC
    const uint8_t truncated[] = { 0xAA, 0x55, 0x10, 0x04, 0x01, 0x02 };
    memcpy(&stream[pos], truncated, sizeof(truncated));
    pos += sizeof(truncated);
    pos += proto_encode(PROTO_PING, (const uint8_t[]){ 1, 0, 0, 0 }, 4, &stream[pos]);
    // valid RELEASE_ALL must decode after resync
    pos += proto_encode(PROTO_RELEASE_ALL, NULL, 0, &stream[pos]);

    proto_decoder_t dec;
    proto_decoder_reset(&dec);
    proto_frame_t frame;
    int frames = 0, crc_errs = 0;
    uint8_t types[8] = { 0 };
    for (size_t i = 0; i < pos; i++) {
        proto_decode_result_t r = proto_decoder_feed(&dec, stream[i], &frame);
        if (r == PROTO_DECODE_FRAME && frames < 8) {
            types[frames++] = frame.type;
        } else if (r == PROTO_DECODE_ERR_BAD_CRC) {
            crc_errs++;
        }
    }
    CHECK(frames == 2, "decoded %d frames want 2", frames);
    CHECK(types[0] == PROTO_GET_STATUS && types[1] == PROTO_RELEASE_ALL,
          "types %02X %02X", types[0], types[1]);
    CHECK(crc_errs >= 1, "expected a CRC error from the truncated frame");
}

static void test_bad_len_rejected(void)
{
    proto_decoder_t dec;
    proto_decoder_reset(&dec);
    proto_frame_t frame;
    const uint8_t bad[] = { 0xAA, 0x55, 0x10, PROTO_MAX_PAYLOAD + 1 };
    int bad_len = 0;
    for (size_t i = 0; i < sizeof(bad); i++) {
        if (proto_decoder_feed(&dec, bad[i], &frame) == PROTO_DECODE_ERR_BAD_LEN) {
            bad_len++;
        }
    }
    CHECK(bad_len == 1, "bad len errors %d want 1", bad_len);
}

static void test_sync_run(void)
{
    uint8_t stream[64];
    size_t pos = 0;
    stream[pos++] = 0xAA;
    stream[pos++] = 0xAA;
    stream[pos++] = 0xAA;
    pos += proto_encode(PROTO_GET_STATUS, NULL, 0, &stream[pos]);

    proto_decoder_t dec;
    proto_decoder_reset(&dec);
    proto_frame_t frame;
    int frames = 0;
    for (size_t i = 0; i < pos; i++) {
        if (proto_decoder_feed(&dec, stream[i], &frame) == PROTO_DECODE_FRAME) {
            frames++;
        }
    }
    CHECK(frames == 1, "decoded %d frames want 1 after sync run", frames);
}

int main(void)
{
    test_crc_vectors();
    test_encode_ping();
    test_decode_roundtrip();
    test_resync_after_garbage();
    test_bad_len_rejected();
    test_sync_run();
    if (failures == 0) {
        printf("protocol.c: all tests pass\n");
        return 0;
    }
    printf("protocol.c: %d FAILURES\n", failures);
    return 1;
}
