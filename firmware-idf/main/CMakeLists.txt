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
