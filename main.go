package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"barista.run"
	"barista.run/bar"
	"barista.run/base/click"
	"barista.run/base/watchers/netlink"
	"barista.run/colors"
	"barista.run/format"
	"barista.run/group/modal"
	"barista.run/modules/battery"
	"barista.run/modules/clock"
	"barista.run/modules/cputemp"
	"barista.run/modules/diskio"
	"barista.run/modules/diskspace"
	"barista.run/modules/media"
	"barista.run/modules/meminfo"
	"barista.run/modules/meta/split"
	"barista.run/modules/netinfo"
	"barista.run/modules/netspeed"
	"barista.run/modules/sysinfo"
	"barista.run/modules/volume"
	"barista.run/modules/volume/alsa"
	"barista.run/modules/weather"
	"barista.run/modules/weather/openweathermap"
	"barista.run/modules/wlan"
	"barista.run/oauth"
	"barista.run/outputs"
	"barista.run/pango"
	"barista.run/pango/icons/fontawesome"

	"github.com/martinlindhe/unit"
	keyring "github.com/zalando/go-keyring"
)

var spacer = pango.Text(" ")
var mainModalController modal.Controller

func truncate(in string, l int) string {
	fromStart := false
	if l < 0 {
		fromStart = true
		l = -l
	}
	inLen := len([]rune(in))
	if inLen <= l {
		return in
	}
	if fromStart {
		return "⋯" + string([]rune(in)[inLen-l+1:])
	}
	return string([]rune(in)[:l-1]) + "⋯"
}

func hms(d time.Duration) (h int, m int, s int) {
	h = int(d.Hours())
	m = int(d.Minutes()) % 60
	s = int(d.Seconds()) % 60
	return
}

func formatMediaTime(d time.Duration) string {
	h, m, s := hms(d)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func makeMediaIconAndPosition(m media.Info) *pango.Node {
	iconAndPosition := pango.Icon("fa-music").Color(colors.Hex("#f70"))
	if m.PlaybackStatus == media.Playing {
		iconAndPosition.Append(spacer,
			pango.Textf("%s/", formatMediaTime(m.Position())))
	}
	if m.PlaybackStatus == media.Paused || m.PlaybackStatus == media.Playing {
		iconAndPosition.Append(spacer,
			pango.Textf("%s", formatMediaTime(m.Length)))
	}
	return iconAndPosition
}

func mediaFormatFunc(m media.Info) bar.Output {
	if m.PlaybackStatus == media.Stopped || m.PlaybackStatus == media.Disconnected {
		return nil
	}
	artist := truncate(m.Artist, 35)
	title := truncate(m.Title, 70-len(artist))
	if len(title) < 35 {
		artist = truncate(m.Artist, 35-len(title))
	}
	var iconAndPosition bar.Output
	if m.PlaybackStatus == media.Playing {
		iconAndPosition = outputs.Repeat(func(time.Time) bar.Output {
			return makeMediaIconAndPosition(m)
		}).Every(time.Second)
	} else {
		iconAndPosition = makeMediaIconAndPosition(m)
	}
	return outputs.Group(outputs.Pango(spacer, iconAndPosition, spacer), outputs.Pango(spacer, title, " - ", artist, spacer))
}

func home(path ...string) string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	args := append([]string{usr.HomeDir}, path...)
	return filepath.Join(args...)
}

func deviceForMountPath(path string) string {
	mnt, _ := exec.Command("df", "-P", path).Output()
	lines := strings.Split(string(mnt), "\n")
	if len(lines) > 1 {
		devAlias := strings.Split(lines[1], " ")[0]
		dev, _ := exec.Command("realpath", devAlias).Output()
		devStr := strings.TrimSpace(string(dev))
		if devStr != "" {
			return devStr
		}
		return devAlias
	}
	return ""
}

type freegeoipResponse struct {
	Lat float64 `json:"latitude"`
	Lng float64 `json:"longitude"`
}

func whereami() (lat float64, lng float64, err error) {
	resp, err := http.Get("https://freegeoip.app/json/")
	if err != nil {
		return 0, 0, err
	}
	var res freegeoipResponse
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return 0, 0, err
	}
	return res.Lat, res.Lng, nil
}

type autoWeatherProvider struct{}

func (a autoWeatherProvider) GetWeather() (weather.Weather, error) {
	lat, lng, err := whereami()
	if err != nil {
		return weather.Weather{}, err
	}
	return openweathermap.
		New("%%OWM_API_KEY%%").
		Coords(lat, lng).
		GetWeather()
}

func setupOauthEncryption() error {
	const service = "barista-sample-bar"
	var username string
	if u, err := user.Current(); err == nil {
		username = u.Username
	} else {
		username = fmt.Sprintf("user-%d", os.Getuid())
	}
	var secretBytes []byte
	// IMPORTANT: The oauth tokens used by some modules are very sensitive, so
	// we encrypt them with a random key and store that random key using
	// libsecret (gnome-keyring or equivalent). If no secret provider is
	// available, there is no way to store tokens (since the version of
	// sample-bar used for setup-oauth will have a different key from the one
	// running in i3bar). See also https://github.com/zalando/go-keyring#linux.
	secret, err := keyring.Get(service, username)
	if err == nil {
		secretBytes, err = base64.RawURLEncoding.DecodeString(secret)
	}
	if err != nil {
		secretBytes = make([]byte, 64)
		_, err := rand.Read(secretBytes)
		if err != nil {
			return err
		}
		secret = base64.RawURLEncoding.EncodeToString(secretBytes)
		err = keyring.Set(service, username, secret)
		if err != nil {
			return err
		}
	}
	oauth.SetEncryptionKey(secretBytes)
	return nil
}

func makeIconOutput(key string) *bar.Segment {
	return outputs.Pango(spacer, pango.Icon(key), spacer)
}

var gsuiteOauthConfig = []byte(`{"installed": {
	"client_id":"%%GOOGLE_CLIENT_ID%%",
	"project_id":"i3-barista",
	"auth_uri":"https://accounts.google.com/o/oauth2/auth",
	"token_uri":"https://www.googleapis.com/oauth2/v3/token",
	"auth_provider_x509_cert_url":"https://www.googleapis.com/oauth2/v1/certs",
	"client_secret":"%%GOOGLE_CLIENT_SECRET%%",
	"redirect_uris":["urn:ietf:wg:oauth:2.0:oob","http://localhost"]
}}`)

func threshold(out *bar.Segment, urgent bool, color ...bool) *bar.Segment {
	if urgent {
		return out.Urgent(true)
	}
	colorKeys := []string{"bad", "degraded", "good"}
	for i, c := range colorKeys {
		if len(color) > i && color[i] {
			return out.Color(colors.Scheme(c))
		}
	}
	return out
}

func main() {
	fontawesome.Load(home("source/Font-Awesome"))
	colors.LoadFromConfig(home(".config/i3/i3status.conf"))

	if err := setupOauthEncryption(); err != nil {
		panic(fmt.Sprintf("Could not setup oauth token encryption: %v", err))
	}

	localdate := clock.Local().
		Output(time.Second, func(now time.Time) bar.Output {
			return outputs.Pango(
				spacer,
				pango.Icon("fa-calendar-day").Alpha(0.6),
				spacer,
				now.Format("Mon Jan 2"),
				spacer,
			).OnClick(click.RunLeft("gsimplecal"))
		})

	localtime := clock.Local().
		Output(time.Second, func(now time.Time) bar.Output {
			return outputs.Text(now.Format(" 15:04:05 ")).
				OnClick(click.Left(func() {
					mainModalController.Toggle("timezones")
				}))
		})

	makeTzClock := func(lbl, tzName string) bar.Module {
		c, err := clock.ZoneByName(tzName)
		if err != nil {
			panic(err)
		}
		return c.Output(time.Minute, func(now time.Time) bar.Output {
			return outputs.Pango(spacer, pango.Text(lbl).Smaller(), spacer, now.Format("15:04"), spacer)
		})
	}

	battSummary, battDetail := split.New(battery.All().Output(func(i battery.Info) bar.Output {
		if i.Status == battery.Disconnected || i.Status == battery.Unknown {
			return nil
		}
		iconName := "battery"
		if i.Status == battery.Charging {
			iconName += "-bolt"
		} else {
			switch {
			case i.RemainingPct() <= 10:
				iconName += "-low"
			case i.RemainingPct() <= 25:
				iconName += "-quarter"
			case i.RemainingPct() <= 50:
				iconName += "-half"
			case i.RemainingPct() <= 75:
				iconName += "-three-quarters"
			default:
				iconName += "-full"
			}
		}
		mainModalController.SetOutput("battery", makeIconOutput("fa-"+iconName))
		rem := i.RemainingTime()
		out := outputs.Group()
		// First segment will be used in summary mode.
		out.Append(outputs.Pango(
			spacer,
			pango.Icon("fa-"+iconName).Alpha(0.6),
			spacer,
			pango.Textf("%d%%", i.RemainingPct()),
			spacer,
		).OnClick(click.Left(func() {
			mainModalController.Toggle("battery")
		})))
		// Others in detail mode.
		out.Append(outputs.Pango(
			spacer,
			pango.Icon("fa-"+iconName).Alpha(0.6),
			spacer,
			pango.Textf("%d%%", i.RemainingPct()),
			spacer,
			pango.Textf("(%d:%02d)", int(rem.Hours()), int(rem.Minutes())%60),
			spacer,
		).OnClick(click.Left(func() {
			mainModalController.Toggle("battery")
		})))
		out.Append(outputs.Pango(
			spacer,
			pango.Textf("%4.1f/%4.1f", i.EnergyNow, i.EnergyFull),
			spacer,
			pango.Text("Wh").Smaller(),
			spacer,
		))
		out.Append(outputs.Pango(
			pango.Textf("%+6.2f", i.SignedPower()),
			spacer,
			pango.Text("W").Smaller(),
			spacer,
		))
		switch {
		case i.RemainingPct() <= 5:
			out.Urgent(true)
		case i.RemainingPct() <= 15:
			out.Color(colors.Scheme("bad"))
		case i.RemainingPct() <= 25:
			out.Color(colors.Scheme("degraded"))
		}
		return out
	}), 1)

	wifiName, wifiDetails := split.New(wlan.Any().Output(func(i wlan.Info) bar.Output {
		if !i.Connecting() && !i.Connected() {
			mainModalController.SetOutput("network", makeIconOutput("fa-ethernet"))
			return nil
		}
		mainModalController.SetOutput("network", makeIconOutput("fa-wifi"))
		if i.Connecting() {
			return outputs.Pango(spacer, pango.Icon("fa-wifi").Alpha(0.6), "...", spacer).
				Color(colors.Scheme("degraded"))
		}
		out := outputs.Group()
		// First segment shown in summary mode only.
		out.Append(outputs.Pango(
			spacer,
			pango.Icon("fa-wifi").Alpha(0.6),
			spacer,
			pango.Text(truncate(i.SSID, -9)),
			spacer,
		).OnClick(click.Left(func() {
			mainModalController.Toggle("network")
		})))
		// Full name, frequency, bssid in detail mode
		out.Append(outputs.Pango(
			spacer,
			pango.Icon("fa-wifi").Alpha(0.6),
			spacer,
			pango.Text(i.SSID),
			spacer,
		))
		out.Append(outputs.Textf(" %2.1fG ", i.Frequency.Gigahertz()))
		out.Append(outputs.Pango(
			spacer,
			pango.Icon("fa-tower-broadcast").Alpha(0.8),
			spacer,
			pango.Text(i.AccessPointMAC).Small(),
			spacer,
		))
		return out
	}), 1)

	vol := volume.New(alsa.DefaultMixer()).Output(func(v volume.Volume) bar.Output {
		if v.Mute {
			return outputs.
				Pango(spacer, pango.Icon("fa-volume-mute").Alpha(0.8), spacer, "MUT", spacer).
				Color(colors.Scheme("degraded"))
		}
		iconName := "off"
		pct := v.Pct()
		if pct > 66 {
			iconName = "up"
		} else if pct > 33 {
			iconName = "down"
		}
		return outputs.Pango(
			spacer,
			pango.Icon("fa-volume-"+iconName).Alpha(0.6),
			spacer,
			pango.Textf("%2d%%", pct),
			spacer,
		)
	})

	loadAvg := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		out := outputs.Pango(
			spacer,
			pango.Icon("fa-microchip").Alpha(0.6),
			spacer,
			pango.Textf("%0.2f", s.Loads[0]),
			spacer,
		)
		// Load averages are unusually high for a few minutes after boot.
		if s.Uptime < 10*time.Minute {
			// so don't add colours until 10 minutes after system start.
			return out
		}
		threshold(out,
			s.Loads[0] > 128 || s.Loads[2] > 64,
			s.Loads[0] > 64 || s.Loads[2] > 32,
			s.Loads[0] > 32 || s.Loads[2] > 16,
		)
		out.OnClick(click.Left(func() {
			mainModalController.Toggle("sysinfo")
		}))
		return out
	})

	loadAvgDetail := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		return pango.Textf(" %0.2f %0.2f ", s.Loads[1], s.Loads[2]).Smaller()
	})

	uptime := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		u := s.Uptime
		var uptimeOut *pango.Node
		if u.Hours() < 24 {
			uptimeOut = pango.Textf("%d:%02d",
				int(u.Hours()), int(u.Minutes())%60)
		} else {
			uptimeOut = pango.Textf("%dd%02dh",
				int(u.Hours()/24), int(u.Hours())%24)
		}
		return outputs.Pango(
			spacer, pango.Icon("fa-arrow-trend-up").Alpha(0.6), spacer, uptimeOut, spacer)
	})

	freeMem := meminfo.New().Output(func(m meminfo.Info) bar.Output {
		out := outputs.Pango(
			spacer,
			pango.Icon("fa-memory").Alpha(0.8),
			spacer,
			format.IBytesize(m.Available()),
			spacer,
		)
		freeGigs := m.Available().Gigabytes()
		threshold(out,
			freeGigs < 0.5,
			freeGigs < 1,
			freeGigs < 2,
			freeGigs > 12)
		out.OnClick(click.Left(func() {
			mainModalController.Toggle("sysinfo")
		}))
		return out
	})

	swapMem := meminfo.New().Output(func(m meminfo.Info) bar.Output {
		return outputs.Pango(
			spacer,
			pango.Icon("fa-retweet").Alpha(0.8),
			spacer,
			format.IBytesize(m["SwapTotal"]-m["SwapFree"]),
			spacer,
			pango.Textf("(% 2.0f%%)", (1-m.FreeFrac("Swap"))*100.0).Small(),
			spacer,
		)
	})

	temp := cputemp.New().
		RefreshInterval(2 * time.Second).
		Output(func(temp unit.Temperature) bar.Output {
			icon := "fa-thermometer"
			switch {
			case temp.Celsius() > 90:
				icon = "fa-temperature-high"
			case temp.Celsius() > 70:
				icon += "-full"
			case temp.Celsius() > 60:
				icon += "-half"
			default:
				icon += "-quarter"
			}
			out := outputs.Pango(
				spacer,
				pango.Icon(icon).Alpha(0.6), spacer,
				spacer,
				pango.Textf("%2d℃", int(temp.Celsius())),
				spacer,
			)
			threshold(out,
				temp.Celsius() > 90,
				temp.Celsius() > 70,
				temp.Celsius() > 60,
			)
			return out
		})

	sub := netlink.Any()
	iface := sub.Get().Name
	sub.Unsubscribe()
	netsp := netspeed.New(iface).
		RefreshInterval(2 * time.Second).
		Output(func(s netspeed.Speeds) bar.Output {
			return outputs.Pango(
				spacer,
				pango.Icon("fa-upload").Alpha(0.5), spacer, pango.Textf("%7s", format.Byterate(s.Tx)),
				spacer,
				pango.Icon("fa-download").Alpha(0.5), spacer, pango.Textf("%7s", format.Byterate(s.Rx)),
				spacer,
			)
		})

	net := netinfo.New().Output(func(i netinfo.State) bar.Output {
		if !i.Enabled() {
			return nil
		}
		if i.Connecting() || len(i.IPs) < 1 {
			return outputs.Pango(spacer, i.Name, spacer).Color(colors.Scheme("degraded"))
		}
		return outputs.Group(outputs.Pango(spacer, i.Name, spacer), outputs.Textf(" %s ", i.IPs[0]))
	})

	formatDiskSpace := func(i diskspace.Info, icon string) bar.Output {
		out := outputs.Pango(
			spacer,
			pango.Icon(icon).Alpha(0.7), spacer, format.IBytesize(i.Available), spacer)
		return threshold(out,
			i.Available.Gigabytes() < 1,
			i.AvailFrac() < 0.05,
			i.AvailFrac() < 0.1,
		)
	}

	rootDev := deviceForMountPath("/")
	var homeDiskspace bar.Module
	if deviceForMountPath(home()) != rootDev {
		homeDiskspace = diskspace.New(home()).Output(func(i diskspace.Info) bar.Output {
			return formatDiskSpace(i, "fa-home")
		})
	}
	rootDiskspace := diskspace.New("/").Output(func(i diskspace.Info) bar.Output {
		return formatDiskSpace(i, "fa-hdd")
	})

	mainDiskio := diskio.New(strings.TrimPrefix(rootDev, "/dev/")).
		Output(func(r diskio.IO) bar.Output {
			return pango.Icon("fa-spinner").
				Concat(spacer).
				ConcatText(format.IByterate(r.Total())).
				Concat(spacer)
		})

	mediaSummary, mediaDetail := split.New(media.Auto().Output(mediaFormatFunc), 1)

	mainModal := modal.New()
	sysMode := mainModal.Mode("sysinfo").
		SetOutput(makeIconOutput("fa-chart-area")).
		Add(loadAvg).
		Detail(loadAvgDetail, uptime).
		Add(freeMem).
		Detail(swapMem, temp)
	if homeDiskspace != nil {
		sysMode.Detail(homeDiskspace)
	}
	sysMode.Detail(rootDiskspace, mainDiskio)
	mainModal.Mode("network").
		SetOutput(makeIconOutput("fa-ethernet")).
		Summary(wifiName).
		Detail(wifiDetails, net, netsp)
	mainModal.Mode("media").
		SetOutput(makeIconOutput("fa-music")).
		Add(vol, mediaSummary).
		Detail(mediaDetail)
	mainModal.Mode("battery").
		// Filled in by the battery module if one is available.
		SetOutput(nil).
		Summary(battSummary).
		Detail(battDetail)
	mainModal.Mode("timezones").
		SetOutput(makeIconOutput("fa-clock")).
		Detail(makeTzClock("Sydney", "Australia/Sydney")).
		Detail(makeTzClock("Singapore", "Asia/Singapore")).
		Detail(makeTzClock("London (UTC)", "Europe/London")).
		Detail(makeTzClock("Vancouver", "America/Vancouver")).
		Add(localdate)

	var mm bar.Module
	mm, mainModalController = mainModal.Build()
	panic(barista.Run(mm, localtime))
}
