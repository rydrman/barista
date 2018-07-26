package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/soumya92/barista"
	"github.com/soumya92/barista/bar"
	"github.com/soumya92/barista/colors"
	"github.com/soumya92/barista/modules/battery"
	"github.com/soumya92/barista/modules/clock"
	"github.com/soumya92/barista/modules/cpuload"
	"github.com/soumya92/barista/modules/netspeed"
	"github.com/soumya92/barista/outputs"
	"github.com/soumya92/barista/pango"
	"github.com/soumya92/barista/pango/icons/fontawesome"
	"github.com/soumya92/barista/pango/icons/ionicons"
)

var (
	red           = colors.Hex("#E06C75")
	redStrong     = colors.Hex("#BE5046")
	green         = colors.Hex("#98C379")
	greenStrong   = colors.Hex("#98C379")
	yellow        = colors.Hex("#E5C07B")
	yellowStrong  = colors.Hex("#D19A66")
	blue          = colors.Hex("#61AFEF")
	blueStrong    = colors.Hex("#61AFEF")
	magenta       = colors.Hex("#C678DD")
	magentaStrong = colors.Hex("#C678DD")
	cyan          = colors.Hex("#56B6C2")
	cyanStrong    = colors.Hex("#56B6C2")
	grey          = colors.Hex("#ABB2BF")
	darkGrey      = colors.Hex("#5C6370")

	spacer = pango.Text(" ").XXSmall()
)

func main() {

	usr, err := user.Current()
	failIfError(err)

	home := usr.HomeDir

	err = fontawesome.Load(filepath.Join(home, "source", "Font-Awesome"))
	failIfError(err)

	err = ionicons.Load(filepath.Join(home, "source", "ionicons"))
	failIfError(err)

	netspeedMod := netspeed.New("wlp2s0")
	netspeedMod.OutputFunc(func(speeds netspeed.Speeds) bar.Output {
		tx := pango.Text("↑")
		switch {
		case speeds.Tx.BitsPerSecond() == 0:
			tx.Color(darkGrey)
		case speeds.Tx.KilobitsPerSecond() < 5:
			tx.Color(grey)
		case speeds.Tx.MegabitsPerSecond() > 1:
			tx.Bold().Color(yellow)
		}
		rx := pango.Text("↓")
		switch {
		case speeds.Rx.BitsPerSecond() == 0:
			rx.Color(darkGrey)
		case speeds.Rx.KilobitsPerSecond() < 5:
			rx.Color(grey)
		case speeds.Rx.MegabitsPerSecond() > 1:
			rx.Bold().Color(yellow)
		}
		cmd := exec.Command(
			"/usr/bin/env",
			"sh", "-c",
			"nmcli connection show --active | grep wifi | cut -d' ' -f1",
		)
		out, err := cmd.Output()
		if len(out) == 0 {
			out = []byte("<??>")
		}
		name := pango.Text(strings.TrimSpace(string(out)))
		if err != nil {
			name.Color(redStrong)
		} else {
			name.Color(grey)
		}
		return outputs.Pango(name, spacer, tx, spacer, rx, spacer)
	})
	barista.Add(netspeedMod)

	cpuloadMod := cpuload.New()
	cpuloadMod.OutputFunc(func(load cpuload.LoadAvg) bar.Output {

		out := outputs.Pango(spacer, pango.Textf("%.2f", load[0]), spacer)
		switch {
		case load[0] >= 0.9:
			out.Color(red)
		case load[0] >= 0.5:
			out.Color(yellow)
		default:
			out.Color(green)
		}
		return out

	})
	barista.Add(cpuloadMod)

	batteryMod := battery.New("BAT0")
	batteryMod.OutputFunc(func(info battery.Info) bar.Output {

		icon := pango.Icon("fa-battery-full")
		color := green
		perc := info.Remaining()

		switch {
		case perc >= 0.9:
			color = greenStrong
		case perc <= 0.75:
			icon = pango.Icon("fa-battery-three-quareters")
		case perc <= 0.5:
			icon = pango.Icon("fa-battery-half")
			color = yellow
		case perc <= 0.25:
			icon = pango.Icon("fa-battery-quarter")
			color = red
		case perc < 0.1:
			icon = pango.Icon("fa-battery-empty")
		}

		out := outputs.Pango(
			spacer,
			icon,
			pango.Textf(" %d%%", int(info.Remaining()*100)),
			spacer,
		).Color(color)

		if perc < 0.1 {
			out.Urgent(true)
		}

		return out

	})
	barista.Add(batteryMod)

	clockMod := clock.Local()
	clockMod.OutputFunc(time.Minute, func(t time.Time) bar.Output {
		return outputs.Text(t.Format("Jan 2 15:04")).Color(blue)
	})
	barista.Add(clockMod)

	err = barista.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

func failIfError(err error) {
	if err == nil {
		return
	}
	fmt.Println(err)
	os.Exit(1)
}