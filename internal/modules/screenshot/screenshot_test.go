package screenshot

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestWindowsOn(t *testing.T) {
	screen := application.Rect{X: 1440, Y: 0, Width: 1920, Height: 1080}
	got := windowsOn(screen, []application.Rect{
		{X: 1500, Y: 100, Width: 800, Height: 600}, // inside
		{X: 1000, Y: 50, Width: 600, Height: 400},  // across the left edge
		{X: 0, Y: 0, Width: 800, Height: 600},      // on another screen
	})
	want := []application.Rect{{X: 60, Y: 100, Width: 800, Height: 600}, {X: 0, Y: 50, Width: 160, Height: 400}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("windowsOn = %+v, want %+v", got, want)
	}
}
