package translate

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The system voices are read through SAPI 5. A voice object is made for
// each reading and released after it, so nothing of the speech engine
// stays loaded between readings beyond its DLLs.

var (
	clsidSpVoice       = windows.GUID{Data1: 0x96749377, Data2: 0x3391, Data3: 0x11D2, Data4: [8]byte{0x9E, 0xE3, 0x00, 0xC0, 0x4F, 0x79, 0x73, 0x96}}
	iidISpVoice        = windows.GUID{Data1: 0x6C44DF74, Data2: 0x72B9, Data3: 0x4992, Data4: [8]byte{0xA1, 0xEC, 0xEF, 0x99, 0x6E, 0x04, 0x22, 0xD4}}
	clsidSpObjectToken = windows.GUID{Data1: 0xEF411752, Data2: 0x3736, Data3: 0x4CB4, Data4: [8]byte{0x9C, 0x8C, 0x8E, 0xF4, 0xCC, 0xB5, 0x8E, 0xFE}}
	iidISpObjectToken  = windows.GUID{Data1: 0x14056589, Data2: 0xE16C, Data3: 0x11D2, Data4: [8]byte{0xBB, 0x90, 0x00, 0xC0, 0x4F, 0x8E, 0xE6, 0xC0}}

	procLocaleNameToLCID = windows.NewLazySystemDLL("kernel32.dll").NewProc("LocaleNameToLCID")
)

// Virtual table slots of ISpVoice and ISpObjectToken.
const (
	slotVoiceSetVoice      = 18
	slotVoiceSpeak         = 20
	slotVoiceWaitUntilDone = 32
	slotTokenSetID         = 15
)

const (
	spfAsync            = 1 << 0
	spfPurgeBeforeSpeak = 1 << 1
	spfIsNotXML         = 1 << 4

	localeAllowNeutralNames = 0x08000000
)

// voiceKeys are where voices are registered: the classic SAPI voices and
// the newer ones Windows installs with language packs, whose engine SAPI
// also runs.
var voiceKeys = []string{
	`SOFTWARE\Microsoft\Speech\Voices\Tokens`,
	`SOFTWARE\Microsoft\Speech_OneCore\Voices\Tokens`,
}

// speakSystem reads text aloud in a system voice for lang and returns when
// it is done or ctx is cancelled.
func speakSystem(ctx context.Context, text, lang string) error {
	// The voice is used from the thread that made it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded); succeeded(hr) {
		defer procCoUninitialize.Call()
	}
	voice, err := comCreate(&clsidSpVoice, &iidISpVoice)
	if err != nil {
		return errors.New("无法使用系统声音朗读")
	}
	defer comRelease(voice)
	// Without a voice for the language, the default voice reads it, as the
	// browser did.
	if id := voiceToken(lang); id != "" {
		if token, err := comCreate(&clsidSpObjectToken, &iidISpObjectToken); err == nil {
			p, _ := windows.UTF16PtrFromString(id)
			if succeeded(comCall(token, slotTokenSetID, 0, uintptr(unsafe.Pointer(p)), 0)) {
				comCall(voice, slotVoiceSetVoice, uintptr(token))
			}
			comRelease(token)
		}
	}
	u, err := windows.UTF16FromString(strings.ReplaceAll(text, "\x00", " "))
	if err != nil {
		return err
	}
	if hr := comCall(voice, slotVoiceSpeak, uintptr(unsafe.Pointer(&u[0])), spfAsync|spfPurgeBeforeSpeak|spfIsNotXML, 0); !succeeded(hr) {
		return errors.New("系统声音朗读失败")
	}
	defer runtime.KeepAlive(u)
	for {
		if ctx.Err() != nil {
			// Speaking nothing with the purge flag stops the reading.
			comCall(voice, slotVoiceSpeak, 0, spfPurgeBeforeSpeak, 0)
			return ctx.Err()
		}
		switch hr := comCall(voice, slotVoiceWaitUntilDone, 100); {
		case hr == 0: // S_OK: done; S_FALSE is the timeout
			return nil
		case !succeeded(hr):
			return errors.New("系统声音朗读失败")
		}
	}
}

// voiceToken finds the registered voice that best fits lang, as the token
// id SAPI takes, or "" when no voice speaks it. A voice of the exact
// locale beats one of the same language.
func voiceToken(lang string) string {
	want := localeID(lang)
	if want == 0 {
		return ""
	}
	best, bestScore := "", 0
	for _, path := range voiceKeys {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, name := range names {
			score := voiceScore(path+`\`+name, want)
			if score > bestScore {
				best, bestScore = `HKEY_LOCAL_MACHINE\`+path+`\`+name, score
			}
		}
	}
	return best
}

// voiceScore rates the voice registered at path for the locale want: 2
// for the same locale, 1 for the same language, 0 otherwise.
func voiceScore(path string, want uint32) int {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path+`\Attributes`, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Language")
	if err != nil {
		return 0
	}
	return languageScore(v, want)
}

// languageScore rates a voice's Language attribute, hexadecimal locale ids
// separated by semicolons, for the locale want.
func languageScore(attr string, want uint32) int {
	score := 0
	for _, part := range strings.Split(attr, ";") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 16, 32)
		if err != nil {
			continue
		}
		switch {
		case uint32(id) == want:
			return 2
		case uint32(id)&0x3FF == want&0x3FF:
			score = 1
		}
	}
	return score
}

// localeID turns a language code such as "en" or "zh-CN" into a Windows
// locale id, or 0.
func localeID(lang string) uint32 {
	if lang == "" || lang == autoDetect {
		return 0
	}
	p, err := windows.UTF16PtrFromString(lang)
	if err != nil {
		return 0
	}
	id, _, _ := procLocaleNameToLCID.Call(uintptr(unsafe.Pointer(p)), localeAllowNeutralNames)
	return uint32(id)
}
