package main

import (
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// UI Chrome Icons (monochrome, used with ui.Icon)
var (
	chevronsUpDownSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5.5 6L8 3.5L10.5 6"/><path d="M5.5 10L8 12.5L10.5 10"/></svg>`))
	infoCircleSVG     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.25"/><path d="M8 7.2V11"/><circle cx="8" cy="4.85" r="0.75" fill="currentColor" stroke="none"/></svg>`))
	moreHorizontalSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><circle cx="3.5" cy="8" r="1.25"/><circle cx="8" cy="8" r="1.25"/><circle cx="12.5" cy="8" r="1.25"/></svg>`))
	chevronDownSVG    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4.75 6.25L8 9.5L11.25 6.25"/></svg>`))
	chevronUpSVG      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4.75 9.5L8 6.25L11.25 9.5"/></svg>`))
	plusSVG           = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M8 3.25V12.75M3.25 8H12.75"/></svg>`))
	modelClusterSVG   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><circle cx="8" cy="3.2" r="1.45"/><circle cx="12.1" cy="5.6" r="1.45"/><circle cx="12.1" cy="10.4" r="1.45"/><circle cx="8" cy="12.8" r="1.45"/><circle cx="3.9" cy="10.4" r="1.45"/><circle cx="3.9" cy="5.6" r="1.45"/><circle cx="8" cy="8" r="1.15"/></svg>`))
	globeSVG          = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6"/><path d="M2.2 8H13.8"/><path d="M8 2C9.7 3.7 10.6 5.8 10.6 8C10.6 10.2 9.7 12.3 8 14C6.3 12.3 5.4 10.2 5.4 8C5.4 5.8 6.3 3.7 8 2Z"/></svg>`))
	checkSVG          = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8.5L6.5 11.5L12.5 4.8"/></svg>`))
	closeSmallSVG     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M4.5 4.5L11.5 11.5M11.5 4.5L4.5 11.5"/></svg>`))
)

// Multi-color vector App Icons (rendered with ui.Image at 18x18)
var (
	appIconMessages = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#32D74B"/>
		<path d="M9 4.2C5.9 4.2 3.4 6.2 3.4 8.7C3.4 10.1 4.2 11.3 5.4 12.1L4.7 14.1L6.9 13.0C7.6 13.2 8.3 13.3 9 13.3C12.1 13.3 14.6 11.3 14.6 8.7C14.6 6.2 12.1 4.2 9 4.2Z" fill="#FFFFFF"/>
	</svg>`))

	appIconDia = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect x="0.5" y="0.5" width="17" height="17" rx="3.8" fill="#FFFBEB" stroke="#FDE68A"/>
		<path d="M3.8 12.6C3.8 8.9 6.1 6.2 9 6.2C11.9 6.2 14.2 8.9 14.2 12.6H3.8Z" fill="#F59E0B"/>
	</svg>`))

	appIconT3 = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#231F3E"/>
		<path d="M3.6 5.6H8.4M6 5.6V12.4" stroke="#FFFFFF" stroke-width="1.65" stroke-linecap="round"/>
		<path d="M10.0 6.1C10.5 5.5 12.0 5.5 12.5 6.3C12.9 7.1 12.2 8.0 11.1 8.3C12.3 8.5 13.0 9.4 12.6 10.5C12.1 11.5 10.5 11.6 9.8 10.8" stroke="#A78BFA" stroke-width="1.55" stroke-linecap="round" stroke-linejoin="round"/>
	</svg>`))

	appIconAlma = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect x="0.5" y="0.5" width="17" height="17" rx="3.8" fill="#F4F4F5" stroke="#D4D4D8"/>
		<circle cx="5.4" cy="5.2" r="1.8" fill="#27272A"/>
		<circle cx="12.6" cy="5.2" r="1.8" fill="#27272A"/>
		<circle cx="9" cy="9.6" r="4.6" fill="#FFFFFF" stroke="#27272A" stroke-width="1.3"/>
		<ellipse cx="7.3" cy="9.2" rx="1.1" ry="1.3" fill="#27272A"/>
		<ellipse cx="10.7" cy="9.2" rx="1.1" ry="1.3" fill="#27272A"/>
		<circle cx="9" cy="11.2" r="0.7" fill="#27272A"/>
	</svg>`))

	appIconFinder = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#1D9BF0"/>
		<path d="M9.5 0H13.8C16.1 0 18 1.9 18 4.2V13.8C18 16.1 16.1 18 13.8 18H8.5C8.5 12 9.5 6 9.5 0Z" fill="#67BFFF"/>
		<path d="M9.4 2.5C8.5 6.0 8.1 8.5 7.9 10.2H9.8" stroke="#0F172A" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round"/>
		<circle cx="5.8" cy="6.6" r="0.9" fill="#0F172A"/>
		<circle cx="12.2" cy="6.6" r="0.9" fill="#0F172A"/>
		<path d="M5.2 12.2C6.8 14.0 11.2 14.0 12.8 12.2" stroke="#0F172A" stroke-width="1.25" stroke-linecap="round"/>
	</svg>`))

	appIconWeChat = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#07C160"/>
		<ellipse cx="7.4" cy="7.6" rx="4.2" ry="3.4" fill="#FFFFFF"/>
		<ellipse cx="11.2" cy="10.2" rx="3.6" ry="2.9" fill="#E4FCEC" stroke="#07C160" stroke-width="0.7"/>
		<circle cx="6.0" cy="7.2" r="0.6" fill="#07C160"/>
		<circle cx="8.7" cy="7.2" r="0.6" fill="#07C160"/>
		<circle cx="10.1" cy="10.0" r="0.5" fill="#07C160"/>
		<circle cx="12.3" cy="10.0" r="0.5" fill="#07C160"/>
	</svg>`))

	appIconMole = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect x="0.5" y="0.5" width="17" height="17" rx="3.8" fill="#F5F5F4" stroke="#E7E5E4"/>
		<path d="M4.2 12.8C4.2 8.8 6.2 5.6 9.2 5.6C12.0 5.6 13.8 8.4 13.8 12.8H4.2Z" fill="#292524"/>
		<path d="M7.5 8.6H12.2" stroke="#F59E0B" stroke-width="1.4" stroke-linecap="round"/>
	</svg>`))

	appIconYab = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect x="0.5" y="0.5" width="17" height="17" rx="3.8" fill="#EFF6FF" stroke="#BFDBFE"/>
		<circle cx="9" cy="9" r="5.2" fill="#3B82F6"/>
		<path d="M6.8 11.2L8.3 8.3L11.2 6.8L9.7 9.7L6.8 11.2Z" fill="#FFFFFF"/>
	</svg>`))

	appIconTelegram = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#24A1DE"/>
		<path d="M13.6 5.0L4.2 8.6C3.7 8.8 3.7 9.2 4.1 9.3L6.5 10.1L12.1 6.6C12.4 6.4 12.6 6.5 12.4 6.7L7.9 10.8L7.7 13.2C8.0 13.2 8.1 13.1 8.3 12.9L9.6 11.6L12.1 13.4C12.5 13.7 12.9 13.5 13.0 13.0L14.3 5.6C14.4 5.1 14.0 4.8 13.6 5.0Z" fill="#FFFFFF"/>
	</svg>`))

	appIconKonseki = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#4F46E5"/>
		<path d="M5.2 4.8V13.2M5.2 9.0L11.8 4.8M7.6 7.5L12.4 13.2" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>
	</svg>`))

	appIconGitHub = ui.MustParseSVG([]byte(`<svg viewBox="0 0 18 18" fill="none">
		<rect width="18" height="18" rx="4.2" fill="#18181B"/>
		<circle cx="9" cy="8.5" r="4.5" fill="#FFFFFF"/>
		<path d="M6.2 5.2L5.5 3.4L7.6 4.4M11.8 5.2L12.5 3.4L10.4 4.4" stroke="#FFFFFF" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"/>
	</svg>`))
)

// OS App Icon cache (lazily populated in background on desktop runs)
var (
	osIconMu    sync.RWMutex
	osIconCache = map[string]*ui.Bitmap{}
	osIconOnce  sync.Once
)

func warmOSAppIcons() {
	osIconOnce.Do(func() {
		go func() {
			defer func() { _ = recover() }()
			apps, err := mygo.Shell.InstalledApps()
			if err != nil {
				return
			}
			for _, a := range apps {
				if a.Name == "" || a.Path == "" {
					continue
				}
				pngBytes, err := mygo.Shell.FileIcon(a.Path, 64)
				if err != nil || len(pngBytes) == 0 {
					continue
				}
				bmp, err := ui.DecodeBitmap(pngBytes)
				if err != nil || bmp == nil {
					continue
				}
				osIconMu.Lock()
				osIconCache[strings.ToLower(a.Name)] = bmp
				osIconMu.Unlock()
			}
		}()
	})
}

func builtInAppSVG(appName string) *ui.SVG {
	lower := strings.ToLower(strings.TrimSpace(appName))
	switch {
	case strings.Contains(lower, "message") || strings.Contains(lower, "imessage") || lower == "信息":
		return appIconMessages
	case strings.Contains(lower, "dia"):
		return appIconDia
	case strings.Contains(lower, "t3"):
		return appIconT3
	case strings.Contains(lower, "alma"):
		return appIconAlma
	case strings.Contains(lower, "finder") || lower == "访达":
		return appIconFinder
	case strings.Contains(lower, "wechat") || strings.Contains(lower, "weixin") || lower == "微信":
		return appIconWeChat
	case strings.Contains(lower, "mole") || strings.Contains(lower, "terminal") || strings.Contains(lower, "warp"):
		return appIconMole
	case strings.Contains(lower, "yab") || strings.Contains(lower, "safari"):
		return appIconYab
	case strings.Contains(lower, "telegram"):
		return appIconTelegram
	case strings.Contains(lower, "konseki") || strings.Contains(lower, "痕跡"):
		return appIconKonseki
	case strings.Contains(lower, "github") || strings.Contains(lower, "codex"):
		return appIconGitHub
	default:
		return nil
	}
}

// renderAppIcon renders a single 18x18 rounded app badge with tooltip.
func renderAppIcon(c *ui.Context, appName string, size float32) {
	if svg := builtInAppSVG(appName); svg != nil {
		ui.Image(c, svg).Size(size, size).Radius(4.2).Tooltip(appName)
		return
	}

	osIconMu.RLock()
	bmp := osIconCache[strings.ToLower(strings.TrimSpace(appName))]
	osIconMu.RUnlock()
	if bmp != nil {
		ui.Image(c, bmp).Size(size, size).Radius(4.2).Tooltip(appName)
		return
	}

	// Fallback monogram badge
	palette := []ui.Color{
		ui.Hex("#4F46E5"),
		ui.Hex("#0EA5E9"),
		ui.Hex("#10B981"),
		ui.Hex("#F59E0B"),
		ui.Hex("#EC4899"),
		ui.Hex("#6366F1"),
	}
	h := 0
	for _, r := range appName {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	bg := palette[h%len(palette)]
	initial := "?"
	if runes := []rune(strings.TrimSpace(appName)); len(runes) > 0 {
		initial = strings.ToUpper(string(runes[0]))
	}
	ui.Box(c).Size(size, size).Radius(4.2).Background(bg).Center().Tooltip(appName).Children(func() {
		ui.Text(c, initial).FontSize(size * 0.58).Bold().TextColor(ui.Hex("#FFFFFF"))
	})
}
