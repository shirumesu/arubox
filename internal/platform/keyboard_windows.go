package platform

import "unsafe"

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

// heldModifiers are released before pressing Ctrl-C so the modifiers the
// user still holds from the hotkey do not turn it into another shortcut.
var heldModifiers = []struct {
	vk       uint16
	extended bool
}{
	{0xa0, false}, {0xa1, false}, // shift
	{0xa3, true},                // right control
	{0xa4, false}, {0xa5, true}, // alt
	{0x5b, true}, {0x5c, true}, // windows
}

// pressCopy presses Ctrl-C. Control goes down before any held Alt or
// Windows key is released, so releasing them does not open a menu or the
// Start menu.
func pressCopy() {
	inputs := []keyboardInput{{kind: inputKeyboard, vk: vkLeftControl}}
	for _, m := range heldModifiers {
		if state, _, _ := procGetAsyncKeyState.Call(uintptr(m.vk)); state&0x8000 != 0 {
			flags := uint32(keyeventfKeyUp)
			if m.extended {
				flags |= keyeventfExtended
			}
			inputs = append(inputs, keyboardInput{kind: inputKeyboard, vk: m.vk, flags: flags})
		}
	}
	inputs = append(inputs,
		keyboardInput{kind: inputKeyboard, vk: vkC},
		keyboardInput{kind: inputKeyboard, vk: vkC, flags: keyeventfKeyUp},
		keyboardInput{kind: inputKeyboard, vk: vkLeftControl, flags: keyeventfKeyUp},
	)
	procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
}
