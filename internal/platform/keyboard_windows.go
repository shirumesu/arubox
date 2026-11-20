package platform

import (
	"fmt"
	"time"
	"unsafe"
)

var (
	procSendInput        = user32.NewProc("SendInput")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

const (
	inputKeyboard     = 1
	keyeventfExtended = 0x0001
	keyeventfKeyUp    = 0x0002
	vkLeftControl     = 0xa2
	vkC               = 0x43
	vkInsert          = 0x2d
	vkMenu            = 0x12
	vkLeftWin         = 0x5b
	vkRightWin        = 0x5c
	// vkMask is unassigned, so pressing it does nothing but count as a key.
	vkMask = 0xe8
	// keysUpWait is how long the hotkey's keys may stay held before the copy
	// is sent anyway.
	keysUpWait = 400 * time.Millisecond
)

// keyboardInput is INPUT with a KEYBDINPUT. The union is aligned like the
// pointer in its members, so on 64-bit Windows it starts after 4 bytes of
// padding, and it is as large as its largest member, MOUSEINPUT, which is 8
// bytes longer than KEYBDINPUT: 40 bytes in all there, 28 on 32-bit.
// SendInput refuses any other size.
type keyboardInput struct {
	kind uint32
	ki   keybdInput
	_    [8]byte
}

type keybdInput struct {
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
}

func keyInput(vk uint16, flags uint32) keyboardInput {
	return keyboardInput{kind: inputKeyboard, ki: keybdInput{vk: vk, flags: flags}}
}

func keyDown(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8000 != 0
}

func sendKeys(inputs []keyboardInput) error {
	n, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if int(n) != len(inputs) {
		return fmt.Errorf("模拟按键失败：%w", err)
	}
	return nil
}

// maskModifierRelease presses a key that does nothing while Alt or Windows
// is held. The hotkey's own key never reached the program, so letting go of
// Alt would otherwise open its menu, and of Windows the Start menu.
func maskModifierRelease() error {
	if keyDown(vkMenu) || keyDown(vkLeftWin) || keyDown(vkRightWin) {
		return sendKeys([]keyboardInput{keyInput(vkMask, 0), keyInput(vkMask, keyeventfKeyUp)})
	}
	return nil
}

// waitForKeysUp waits, up to keysUpWait, until no key is held.
func waitForKeysUp() {
	held := func() bool {
		// From backspace up: the codes below are mouse buttons.
		for vk := uint16(0x08); vk <= 0xfe; vk++ {
			if keyDown(vk) {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(keysUpWait)
	for held() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// heldModifiers are released before pressing the copy keys so the modifiers
// the user still holds do not turn it into another shortcut.
var heldModifiers = []struct {
	vk       uint16
	extended bool
}{
	{0xa0, false}, {0xa1, false}, // shift
	{0xa3, true},                // right control
	{0xa4, false}, {0xa5, true}, // alt
	{0x5b, true}, {0x5c, true}, // windows
}

// pressCopy presses Ctrl with key. Control goes down before any held Alt or
// Windows key is released, so releasing them does not open a menu or the
// Start menu.
func pressCopy(key uint16, extended bool) error {
	inputs := []keyboardInput{keyInput(vkLeftControl, 0)}
	for _, m := range heldModifiers {
		if keyDown(m.vk) {
			flags := uint32(keyeventfKeyUp)
			if m.extended {
				flags |= keyeventfExtended
			}
			inputs = append(inputs, keyInput(m.vk, flags))
		}
	}
	var keyFlags uint32
	if extended {
		keyFlags = keyeventfExtended
	}
	inputs = append(inputs,
		keyInput(key, keyFlags),
		keyInput(key, keyFlags|keyeventfKeyUp),
		keyInput(vkLeftControl, keyeventfKeyUp),
	)
	return sendKeys(inputs)
}
