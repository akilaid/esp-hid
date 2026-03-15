//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	serial "go.bug.st/serial"
	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14

	wmQuit        = 0x0012
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMouseWheel  = 0x020A

	llkhfInjected = 0x10
	wheelDelta    = 120

	vkBack      = 0x08
	vkTab       = 0x09
	vkReturn    = 0x0D
	vkShift     = 0x10
	vkControl   = 0x11
	vkMenu      = 0x12
	vkCapital   = 0x14
	vkEscape    = 0x1B
	vkSpace     = 0x20
	vkPrior     = 0x21
	vkNext      = 0x22
	vkEnd       = 0x23
	vkHome      = 0x24
	vkLeft      = 0x25
	vkUp        = 0x26
	vkRight     = 0x27
	vkDown      = 0x28
	vkSnapshot  = 0x2C
	vkInsert    = 0x2D
	vkDelete    = 0x2E
	vk0         = 0x30
	vk9         = 0x39
	vkA         = 0x41
	vkZ         = 0x5A
	vkLWin      = 0x5B
	vkRWin      = 0x5C
	vkNumpad0   = 0x60
	vkNumpad1   = 0x61
	vkNumpad2   = 0x62
	vkNumpad3   = 0x63
	vkNumpad4   = 0x64
	vkNumpad5   = 0x65
	vkNumpad6   = 0x66
	vkNumpad7   = 0x67
	vkNumpad8   = 0x68
	vkNumpad9   = 0x69
	vkMultiply  = 0x6A
	vkAdd       = 0x6B
	vkSubtract  = 0x6D
	vkDecimal   = 0x6E
	vkDivide    = 0x6F
	vkF1        = 0x70
	vkF12       = 0x7B
	vkF13       = 0x7C
	vkF24       = 0x87
	vkLShift    = 0xA0
	vkRShift    = 0xA1
	vkLControl  = 0xA2
	vkRControl  = 0xA3
	vkLMenu     = 0xA4
	vkRMenu     = 0xA5
	vkOEM1      = 0xBA
	vkOEMPlus   = 0xBB
	vkOEMComma  = 0xBC
	vkOEMMinus  = 0xBD
	vkOEMPeriod = 0xBE
	vkOEM2      = 0xBF
	vkOEM3      = 0xC0
	vkOEM4      = 0xDB
	vkOEM5      = 0xDC
	vkOEM6      = 0xDD
	vkOEM7      = 0xDE
	vkOEM102    = 0xE2
)

const (
	bleKeyLeftCtrl   = 0x80
	bleKeyLeftShift  = 0x81
	bleKeyLeftAlt    = 0x82
	bleKeyLeftGUI    = 0x83
	bleKeyRightCtrl  = 0x84
	bleKeyRightShift = 0x85
	bleKeyRightAlt   = 0x86
	bleKeyRightGUI   = 0x87

	bleKeyReturn    = 0xB0
	bleKeyEsc       = 0xB1
	bleKeyBackspace = 0xB2
	bleKeyTab       = 0xB3

	bleKeyCapsLock = 0xC1
	bleKeyF1       = 0xC2
	bleKeyF13      = 0xF0

	bleKeyPrtSc    = 0xCE
	bleKeyInsert   = 0xD1
	bleKeyHome     = 0xD2
	bleKeyPageUp   = 0xD3
	bleKeyDelete   = 0xD4
	bleKeyEnd      = 0xD5
	bleKeyPageDown = 0xD6
	bleKeyRight    = 0xD7
	bleKeyLeft     = 0xD8
	bleKeyDown     = 0xD9
	bleKeyUp       = 0xDA

	bleKeyNum0        = 0xEA
	bleKeyNum1        = 0xE1
	bleKeyNum2        = 0xE2
	bleKeyNum3        = 0xE3
	bleKeyNum4        = 0xE4
	bleKeyNum5        = 0xE5
	bleKeyNum6        = 0xE6
	bleKeyNum7        = 0xE7
	bleKeyNum8        = 0xE8
	bleKeyNum9        = 0xE9
	bleKeyNumSlash    = 0xDC
	bleKeyNumAsterisk = 0xDD
	bleKeyNumMinus    = 0xDE
	bleKeyNumPlus     = 0xDF
	bleKeyNumEnter    = 0xE0
	bleKeyNumPeriod   = 0xEB
)

type point struct {
	X int32
	Y int32
}

type windowsMessage struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type msllHookStruct struct {
	Pt          point
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type kbdllHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type inputEventKind int

const (
	inputMouseMoveEvent inputEventKind = iota + 1
	inputMouseLeftClickEvent
	inputMouseRightClickEvent
	inputMouseScrollEvent
	inputKeyboardDownEvent
	inputKeyboardUpEvent
)

type inputEvent struct {
	kind    inputEventKind
	x       int
	y       int
	scroll  int
	keyCode uint8
}

type config struct {
	portName        string
	autoPort        bool
	baudRate        int
	moveRateHz      int
	reconnectDelay  time.Duration
	captureKeyboard bool
}

type movementAccumulator struct {
	mu          sync.Mutex
	initialized bool
	lastX       int
	lastY       int
	pendingDX   int
	pendingDY   int
}

func (a *movementAccumulator) addAbsolutePosition(x, y int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized {
		a.initialized = true
		a.lastX = x
		a.lastY = y
		return
	}

	a.pendingDX += x - a.lastX
	a.pendingDY += y - a.lastY
	a.lastX = x
	a.lastY = y
}

func (a *movementAccumulator) drain() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	dx := a.pendingDX
	dy := a.pendingDY
	a.pendingDX = 0
	a.pendingDY = 0
	return dx, dy
}

func comPortNumber(port string) (int, bool) {
	name := strings.ToUpper(strings.TrimSpace(port))
	if !strings.HasPrefix(name, "COM") {
		return 0, false
	}

	number, err := strconv.Atoi(strings.TrimPrefix(name, "COM"))
	if err != nil || number <= 0 {
		return 0, false
	}

	return number, true
}

func listSerialPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}

	sort.Slice(ports, func(i, j int) bool {
		leftNumber, leftIsCOM := comPortNumber(ports[i])
		rightNumber, rightIsCOM := comPortNumber(ports[j])

		if leftIsCOM && rightIsCOM {
			return leftNumber < rightNumber
		}
		if leftIsCOM != rightIsCOM {
			return leftIsCOM
		}

		return strings.ToUpper(ports[i]) < strings.ToUpper(ports[j])
	})

	return ports, nil
}

func portsToString(ports []string) string {
	if len(ports) == 0 {
		return "none"
	}
	return strings.Join(ports, ", ")
}

func autoSelectPort() (string, error) {
	ports, err := listSerialPorts()
	if err != nil {
		return "", fmt.Errorf("listing serial ports failed: %w", err)
	}
	if len(ports) == 0 {
		return "", errors.New("no serial ports available")
	}

	selected := ports[0]
	highestComNumber := -1
	for _, port := range ports {
		if number, ok := comPortNumber(port); ok && number > highestComNumber {
			highestComNumber = number
			selected = port
		}
	}

	return selected, nil
}

func enqueueCommand(queue chan string, command string) {
	select {
	case queue <- command:
		return
	default:
	}

	if strings.HasPrefix(command, "MOVE ") {
		return
	}

	select {
	case <-queue:
	default:
	}

	select {
	case queue <- command:
	default:
	}
}

func publishInputEvent(out chan<- inputEvent, event inputEvent) {
	select {
	case out <- event:
	default:
	}
}

func normalizeWheel(mouseData uint32) int {
	delta := int16(mouseData >> 16)
	if delta == 0 {
		return 0
	}

	steps := int(delta) / wheelDelta
	if steps == 0 {
		if delta > 0 {
			return 1
		}
		return -1
	}

	return steps
}

func vkToBleKeyCode(vkCode uint32) (uint8, bool) {
	switch {
	case vkCode >= vkA && vkCode <= vkZ:
		return uint8('a' + (vkCode - vkA)), true
	case vkCode >= vk0 && vkCode <= vk9:
		return uint8('0' + (vkCode - vk0)), true
	case vkCode >= vkF1 && vkCode <= vkF12:
		return uint8(bleKeyF1 + (vkCode - vkF1)), true
	case vkCode >= vkF13 && vkCode <= vkF24:
		return uint8(bleKeyF13 + (vkCode - vkF13)), true
	}

	switch vkCode {
	case vkSpace:
		return ' ', true
	case vkBack:
		return bleKeyBackspace, true
	case vkTab:
		return bleKeyTab, true
	case vkReturn:
		return bleKeyReturn, true
	case vkEscape:
		return bleKeyEsc, true
	case vkInsert:
		return bleKeyInsert, true
	case vkDelete:
		return bleKeyDelete, true
	case vkHome:
		return bleKeyHome, true
	case vkEnd:
		return bleKeyEnd, true
	case vkPrior:
		return bleKeyPageUp, true
	case vkNext:
		return bleKeyPageDown, true
	case vkLeft:
		return bleKeyLeft, true
	case vkRight:
		return bleKeyRight, true
	case vkUp:
		return bleKeyUp, true
	case vkDown:
		return bleKeyDown, true
	case vkCapital:
		return bleKeyCapsLock, true
	case vkSnapshot:
		return bleKeyPrtSc, true
	case vkLControl:
		return bleKeyLeftCtrl, true
	case vkRControl:
		return bleKeyRightCtrl, true
	case vkControl:
		return bleKeyLeftCtrl, true
	case vkLShift:
		return bleKeyLeftShift, true
	case vkRShift:
		return bleKeyRightShift, true
	case vkShift:
		return bleKeyLeftShift, true
	case vkLMenu:
		return bleKeyLeftAlt, true
	case vkRMenu:
		return bleKeyRightAlt, true
	case vkMenu:
		return bleKeyLeftAlt, true
	case vkLWin:
		return bleKeyLeftGUI, true
	case vkRWin:
		return bleKeyRightGUI, true
	case vkNumpad0:
		return bleKeyNum0, true
	case vkNumpad1:
		return bleKeyNum1, true
	case vkNumpad2:
		return bleKeyNum2, true
	case vkNumpad3:
		return bleKeyNum3, true
	case vkNumpad4:
		return bleKeyNum4, true
	case vkNumpad5:
		return bleKeyNum5, true
	case vkNumpad6:
		return bleKeyNum6, true
	case vkNumpad7:
		return bleKeyNum7, true
	case vkNumpad8:
		return bleKeyNum8, true
	case vkNumpad9:
		return bleKeyNum9, true
	case vkDivide:
		return bleKeyNumSlash, true
	case vkMultiply:
		return bleKeyNumAsterisk, true
	case vkSubtract:
		return bleKeyNumMinus, true
	case vkAdd:
		return bleKeyNumPlus, true
	case vkDecimal:
		return bleKeyNumPeriod, true
	case vkOEMMinus:
		return '-', true
	case vkOEMPlus:
		return '=', true
	case vkOEM4:
		return '[', true
	case vkOEM6:
		return ']', true
	case vkOEM5:
		return '\\', true
	case vkOEM1:
		return ';', true
	case vkOEM7:
		return '\'', true
	case vkOEM3:
		return '`', true
	case vkOEMComma:
		return ',', true
	case vkOEMPeriod:
		return '.', true
	case vkOEM2:
		return '/', true
	case vkOEM102:
		return '\\', true
	}

	return 0, false
}

func runInputHooks(ctx context.Context, captureKeyboard bool, out chan<- inputEvent) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	mouseCallback := windows.NewCallback(func(nCode uintptr, wParam uintptr, lParam *msllHookStruct) uintptr {
		lParamAddress := uintptr(0)
		if lParam != nil {
			lParamAddress = uintptr(unsafe.Pointer(lParam))
		}

		if int32(nCode) >= 0 {
			if lParam == nil {
				next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
				return next
			}

			switch uint32(wParam) {
			case wmMouseMove:
				publishInputEvent(out, inputEvent{
					kind: inputMouseMoveEvent,
					x:    int(lParam.Pt.X),
					y:    int(lParam.Pt.Y),
				})
			case wmLButtonDown:
				publishInputEvent(out, inputEvent{kind: inputMouseLeftClickEvent})
			case wmRButtonDown:
				publishInputEvent(out, inputEvent{kind: inputMouseRightClickEvent})
			case wmMouseWheel:
				scrollAmount := normalizeWheel(lParam.MouseData)
				if scrollAmount != 0 {
					publishInputEvent(out, inputEvent{kind: inputMouseScrollEvent, scroll: scrollAmount})
				}
			}
		}

		next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
		return next
	})

	keyboardCallback := windows.NewCallback(func(nCode uintptr, wParam uintptr, lParam *kbdllHookStruct) uintptr {
		lParamAddress := uintptr(0)
		if lParam != nil {
			lParamAddress = uintptr(unsafe.Pointer(lParam))
		}

		if captureKeyboard && int32(nCode) >= 0 {
			if lParam == nil {
				next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
				return next
			}

			if (lParam.Flags & llkhfInjected) == 0 {
				keyCode, mapped := vkToBleKeyCode(lParam.VkCode)
				if mapped {
					switch uint32(wParam) {
					case wmKeyDown, wmSysKeyDown:
						publishInputEvent(out, inputEvent{kind: inputKeyboardDownEvent, keyCode: keyCode})
					case wmKeyUp, wmSysKeyUp:
						publishInputEvent(out, inputEvent{kind: inputKeyboardUpEvent, keyCode: keyCode})
					}
				}
			}
		}

		next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
		return next
	})

	mouseHookHandle, _, mouseHookErr := procSetWindowsHookExW.Call(uintptr(whMouseLL), mouseCallback, 0, 0)
	if mouseHookHandle == 0 {
		if mouseHookErr != syscall.Errno(0) {
			return fmt.Errorf("SetWindowsHookExW(mouse) failed: %w", mouseHookErr)
		}
		return errors.New("SetWindowsHookExW(mouse) returned null hook handle")
	}
	defer procUnhookWindowsHookEx.Call(mouseHookHandle)

	if captureKeyboard {
		keyboardHookHandle, _, keyboardHookErr := procSetWindowsHookExW.Call(uintptr(whKeyboardLL), keyboardCallback, 0, 0)
		if keyboardHookHandle == 0 {
			if keyboardHookErr != syscall.Errno(0) {
				return fmt.Errorf("SetWindowsHookExW(keyboard) failed: %w", keyboardHookErr)
			}
			return errors.New("SetWindowsHookExW(keyboard) returned null hook handle")
		}
		defer procUnhookWindowsHookEx.Call(keyboardHookHandle)
	}

	threadID := windows.GetCurrentThreadId()
	stopSignal := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			procPostThreadMessageW.Call(uintptr(threadID), uintptr(wmQuit), 0, 0)
		case <-stopSignal:
		}
	}()
	defer close(stopSignal)

	var message windowsMessage
	for {
		ret, _, messageErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		switch int32(ret) {
		case -1:
			if messageErr != syscall.Errno(0) {
				return fmt.Errorf("GetMessageW failed: %w", messageErr)
			}
			return errors.New("GetMessageW failed")
		case 0:
			return nil
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}

func sendResetState(port serial.Port) error {
	_, err := io.WriteString(port, "RELEASE\nKEYRELEASE\n")
	return err
}

func writeLoop(ctx context.Context, cfg config, queue <-chan string) {
	var port serial.Port
	activePortName := cfg.portName
	defer func() {
		if port != nil {
			_ = port.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case command, ok := <-queue:
			if !ok {
				return
			}

			for {
				if port == nil {
					targetPort := cfg.portName
					if cfg.autoPort {
						autoPort, autoErr := autoSelectPort()
						if autoErr != nil {
							log.Printf("serial auto-detect failed: %v", autoErr)
							if !sleepWithContext(ctx, cfg.reconnectDelay) {
								return
							}
							continue
						}
						targetPort = autoPort
					}

					openedPort, err := serial.Open(targetPort, &serial.Mode{BaudRate: cfg.baudRate})
					if err != nil {
						available := "unavailable"
						if ports, listErr := listSerialPorts(); listErr == nil {
							available = portsToString(ports)
						}
						log.Printf("serial open failed on %s: %v (available: %s)", targetPort, err, available)
						if !sleepWithContext(ctx, cfg.reconnectDelay) {
							return
						}
						continue
					}

					if err := sendResetState(openedPort); err != nil {
						log.Printf("serial init write failed on %s: %v", targetPort, err)
						_ = openedPort.Close()
						if !sleepWithContext(ctx, cfg.reconnectDelay) {
							return
						}
						continue
					}

					port = openedPort
					activePortName = targetPort
					log.Printf("serial connected on %s at %d baud", activePortName, cfg.baudRate)
				}

				if _, err := io.WriteString(port, command+"\n"); err != nil {
					log.Printf("serial write failed on %s: %v", activePortName, err)
					_ = port.Close()
					port = nil

					if !sleepWithContext(ctx, cfg.reconnectDelay) {
						return
					}
					continue
				}

				break
			}
		}
	}
}

func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func handleInputEvent(event inputEvent, accumulator *movementAccumulator, queue chan string) {
	switch event.kind {
	case inputMouseMoveEvent:
		accumulator.addAbsolutePosition(event.x, event.y)
	case inputMouseLeftClickEvent:
		enqueueCommand(queue, "CLICK LEFT")
	case inputMouseRightClickEvent:
		enqueueCommand(queue, "CLICK RIGHT")
	case inputMouseScrollEvent:
		if event.scroll != 0 {
			enqueueCommand(queue, fmt.Sprintf("SCROLL %d", event.scroll))
		}
	case inputKeyboardDownEvent:
		enqueueCommand(queue, fmt.Sprintf("KEYDOWN %d", event.keyCode))
	case inputKeyboardUpEvent:
		enqueueCommand(queue, fmt.Sprintf("KEYUP %d", event.keyCode))
	}
}

func runCaptureLoop(ctx context.Context, cfg config, queue chan string) error {
	if cfg.moveRateHz <= 0 {
		return errors.New("move rate must be greater than 0")
	}

	accumulator := &movementAccumulator{}
	eventChannel := make(chan inputEvent, 4096)
	errorChannel := make(chan error, 1)

	go func() {
		errorChannel <- runInputHooks(ctx, cfg.captureKeyboard, eventChannel)
		close(eventChannel)
		close(errorChannel)
	}()

	ticker := time.NewTicker(time.Second / time.Duration(cfg.moveRateHz))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case hookErr, ok := <-errorChannel:
			if ok && hookErr != nil {
				return hookErr
			}
			return nil
		case <-ticker.C:
			dx, dy := accumulator.drain()
			if dx != 0 || dy != 0 {
				enqueueCommand(queue, fmt.Sprintf("MOVE %d %d", dx, dy))
			}
		case event, ok := <-eventChannel:
			if !ok {
				return nil
			}
			handleInputEvent(event, accumulator, queue)
		}
	}
}

func parseConfig() (config, error) {
	port := flag.String("port", "auto", "Serial COM port connected to the ESP32 (or 'auto')")
	baud := flag.Int("baud", 115200, "Serial baud rate")
	rate := flag.Int("rate", 120, "Maximum move send rate (events per second)")
	reconnect := flag.Duration("reconnect", 750*time.Millisecond, "Reconnect delay after serial failure")
	keyboard := flag.Bool("keyboard", true, "Capture and forward keyboard key down/up events")
	flag.Parse()

	autoPort := strings.EqualFold(*port, "auto")

	cfg := config{
		portName:        *port,
		autoPort:        autoPort,
		baudRate:        *baud,
		moveRateHz:      *rate,
		reconnectDelay:  *reconnect,
		captureKeyboard: *keyboard,
	}

	if cfg.portName == "" && !cfg.autoPort {
		return cfg, errors.New("port cannot be empty")
	}
	if cfg.baudRate <= 0 {
		return cfg, errors.New("baud must be greater than 0")
	}
	if cfg.moveRateHz <= 0 {
		return cfg, errors.New("rate must be greater than 0")
	}
	if cfg.reconnectDelay <= 0 {
		return cfg, errors.New("reconnect delay must be greater than 0")
	}

	return cfg, nil
}

func startupPortHint(cfg config) {
	ports, err := listSerialPorts()
	if err != nil {
		log.Printf("unable to list serial ports: %v", err)
		return
	}

	if cfg.autoPort {
		selected, selectErr := autoSelectPort()
		if selectErr != nil {
			log.Printf("serial auto-detect: %v", selectErr)
			return
		}
		log.Printf("serial auto-detect selected %s (available: %s)", selected, portsToString(ports))
		return
	}

	requested := strings.ToUpper(cfg.portName)
	found := false
	for _, port := range ports {
		if strings.ToUpper(port) == requested {
			found = true
			break
		}
	}

	if !found {
		log.Printf("warning: requested serial port %s not currently present (available: %s)", cfg.portName, portsToString(ports))
	}
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("starting mouse/keyboard bridge: port=%s baud=%d rate=%dHz keyboard=%t", cfg.portName, cfg.baudRate, cfg.moveRateHz, cfg.captureKeyboard)
	startupPortHint(cfg)

	commandQueue := make(chan string, 1024)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeLoop(ctx, cfg, commandQueue)
	}()

	if err := runCaptureLoop(ctx, cfg, commandQueue); err != nil {
		log.Printf("capture loop stopped: %v", err)
	}

	enqueueCommand(commandQueue, "RELEASE")
	enqueueCommand(commandQueue, "KEYRELEASE")
	close(commandQueue)
	cancel()
	wg.Wait()
}
