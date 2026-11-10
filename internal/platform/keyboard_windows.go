package platform

import (
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

// keyboardInput is INPUT with a KEYBDINPUT, padded to the size of the union's
// largest member.
type keyboardInput struct {
	kind      uint32
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
	_         [8]byte
}

func keyDown(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8000 != 0
}

func sendKeys(inputs []keyboardInput) {
	procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
}

// maskModifierRelease presses a key that does nothing while Alt or Windows
// is held. The hotkey's own key never reached the program, so letting go of
// Alt would otherwise open its menu, and of Windows the Start menu.
func maskModifierRelease() {
	if keyDown(vkMenu) || keyDown(vkLeftWin) || keyDown(vkRightWin) {
		sendKeys([]keyboardInput{
			{kind: inputKeyboard, vk: vkMask},
			{kind: inputKeyboard, vk: vkMask, flags: keyeventfKeyUp},
		})
	}
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
func pressCopy(key uint16, extended bool) {
	inputs := []keyboardInput{{kind: inputKeyboard, vk: vkLeftControl}}
	for _, m := range heldModifiers {
		if keyDown(m.vk) {
			flags := uint32(keyeventfKeyUp)
			if m.extended {
				flags |= keyeventfExtended
			}
			inputs = append(inputs, keyboardInput{kind: inputKeyboard, vk: m.vk, flags: flags})
		}
	}
	var keyFlags uint32
	if extended {
		keyFlags = keyeventfExtended
	}
	inputs = append(inputs,
		keyboardInput{kind: inputKeyboard, vk: key, flags: keyFlags},
		keyboardInput{kind: inputKeyboard, vk: key, flags: keyFlags | keyeventfKeyUp},
		keyboardInput{kind: inputKeyboard, vk: vkLeftControl, flags: keyeventfKeyUp},
	)
	sendKeys(inputs)
}
