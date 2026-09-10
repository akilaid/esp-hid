idf_component_register(
    SRCS
        "main.c"
        "protocol.c"
        "usb_link.c"
        "status_led.c"
        "ble_hid.c"
        "hid_reports.c"
    INCLUDE_DIRS "."
    PRIV_REQUIRES
        esp_hid
        bt
        esp_driver_usb_serial_jtag
        esp_driver_gpio
        nvs_flash
        esp_event
        esp_timer
)

# --- Version reported in HELLO ---------------------------------------------
# The three bytes HELLO carries were hardcoded to 1.0.0 and drifted silently
# through eleven releases. Derive them from the build instead.
#
# BRIDGE_FW_VERSION wins when supplied. The release workflow passes the tag it
# is *about* to publish, which git cannot know at build time — the tag is
# created by the release job afterwards, so git describe would report the
# previous release plus a commit count.
#
# Otherwise fall back to PROJECT_VER, which IDF derives from git describe. That
# is what makes a local build report something truthful rather than 0.0.0.
if(NOT DEFINED BRIDGE_FW_VERSION OR BRIDGE_FW_VERSION STREQUAL "")
    set(BRIDGE_FW_VERSION "$ENV{BRIDGE_FW_VERSION}")
endif()
if(BRIDGE_FW_VERSION STREQUAL "")
    idf_build_get_property(BRIDGE_FW_VERSION PROJECT_VER)
endif()

# Tolerant on purpose: matches 2.1.0 out of "v2.1.0", "v2.1.0-dirty" and
# "v2.1.0-3-gabc1234" alike, so a build from an untagged commit still reports
# the release it descends from.
if(BRIDGE_FW_VERSION MATCHES "([0-9]+)\\.([0-9]+)\\.([0-9]+)")
    set(bridge_fw_major ${CMAKE_MATCH_1})
    set(bridge_fw_minor ${CMAKE_MATCH_2})
    set(bridge_fw_patch ${CMAKE_MATCH_3})
    message(STATUS "Bridge firmware version: "
                   "${bridge_fw_major}.${bridge_fw_minor}.${bridge_fw_patch} "
                   "(from '${BRIDGE_FW_VERSION}')")
else()
    # Never fail the build over a version string; report 0.0.0, which is
    # visibly wrong in the GUI rather than quietly plausible.
    set(bridge_fw_major 0)
    set(bridge_fw_minor 0)
    set(bridge_fw_patch 0)
    message(WARNING "Could not parse a version from '${BRIDGE_FW_VERSION}'; "
                    "HELLO will report 0.0.0")
endif()

target_compile_definitions(${COMPONENT_LIB} PRIVATE
    BRIDGE_FW_MAJOR=${bridge_fw_major}
    BRIDGE_FW_MINOR=${bridge_fw_minor}
    BRIDGE_FW_PATCH=${bridge_fw_patch}
)
