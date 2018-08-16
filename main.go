package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lucasb-eyer/go-colorful"

	"github.com/soumya92/barista/modules/sysinfo"

	"github.com/soumya92/barista/modules/volume"

	"github.com/soumya92/barista"
	"github.com/soumya92/barista/bar"
	"github.com/soumya92/barista/colors"
	"github.com/soumya92/barista/modules/battery"
	"github.com/soumya92/barista/modules/clock"
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
	greenStrong   = colors.Hex("#88B369")
	yellow        = colors.Hex("#EEEE00")
	beige         = colors.Hex("#E5C07B")
	burntOrange   = colors.Hex("#D19A66")
	blue          = colors.Hex("#61AFEF")
	blueStrong    = colors.Hex("#519FDF")
	magenta       = colors.Hex("#C678DD")
	magentaStrong = colors.Hex("#B668CD")
	cyan          = colors.Hex("#56B6C2")
	cyanStrong    = colors.Hex("#46A6B2")
	grey          = colors.Hex("#ABB2BF")
	darkGrey      = colors.Hex("#5C6370")

	spacer = pango.Text(" ").XXSmall()

	conf = config{
		NetInterface: "eth0",
	}
)

type config struct {
	NetInterface string `json:"net.iface"`
}

func main() {

	usr, err := user.Current()
	failIfError(err)

	home := usr.HomeDir
	f, err := os.Open(filepath.Join(home, ".config/barista/config"))
	if os.IsNotExist(err) {
		// leave defaults
	} else {
		err = json.NewDecoder(f).Decode(&conf)
		f.Close() // nolint: gas
		failIfError(err)
	}

	err = fontawesome.Load(filepath.Join(home, "source", "Font-Awesome"))
	failIfError(err)

	err = ionicons.Load(filepath.Join(home, "source", "ionicons"))
	failIfError(err)

	netspeedMod := netspeed.New(conf.NetInterface)
	netspeedMod.Output(renderNet)
	barista.Add(netspeedMod)

	sysInfoMod := sysinfo.New()
	sysInfoMod.Output(renderSysInfo)
	barista.Add(sysInfoMod)

	batteryMod := battery.All()
	batteryMod.Output(renderBattery)
	barista.Add(batteryMod)

	clockMod := clock.Local()
	clockMod.Output(time.Second, renderTime)
	barista.Add(clockMod)

	volumeMod := volume.DefaultMixer()
	volumeMod.Output(renderVolume)
	barista.Add(volumeMod)

	err = barista.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

func renderNet(speeds netspeed.Speeds) bar.Output {

	tx := pango.Text("↑")
	switch {
	case speeds.Tx.BitsPerSecond() == 0:
		tx.Color(darkGrey)
	case speeds.Tx.KilobitsPerSecond() < 5:
		tx.Color(grey)
	case speeds.Tx.MegabitsPerSecond() > 1:
		tx.Bold().Color(beige)
	}
	rx := pango.Text("↓")
	switch {
	case speeds.Rx.BitsPerSecond() == 0:
		rx.Color(darkGrey)
	case speeds.Rx.KilobitsPerSecond() < 5:
		rx.Color(grey)
	case speeds.Rx.MegabitsPerSecond() > 1:
		rx.Bold().Color(beige)
	}
	cmd := exec.Command( // nolint: gas
		"/usr/bin/env",
		"sh", "-c",
		"nmcli --fields=DEVICE,NAME connection show --active | grep "+conf.NetInterface+" | cut -d' ' -f3-",
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

}

func renderSysInfo(info sysinfo.Info) bar.Output {

	load := info.Loads[0] / float64(runtime.NumCPU())
	out := outputs.Pango(
		spacer,
		pango.Textf("%2d%%", int(load*100)),
		spacer,
	)
	switch {
	case load >= 0.9:
		out.Color(red)
	case load >= 0.5:
		out.Color(beige)
	default:
		out.Color(green)
	}
	return out

}

func renderBattery(info battery.Info) bar.Output {

	if info.Status == battery.Disconnected {
		return nil
	}

	icon := pango.Icon("fa-battery-full")
	color := green
	perc := info.Remaining()

	switch {
	case perc < 0.1:
		icon = pango.Icon("fa-battery-empty")
	case perc <= 0.25:
		icon = pango.Icon("fa-battery-quarter")
		color = red
	case perc <= 0.5:
		icon = pango.Icon("fa-battery-half")
		color = beige
	case perc <= 0.75:
		icon = pango.Icon("fa-battery-three-quarters")
	case perc >= 0.9:
		color = greenStrong
	}

	nodes := []interface{}{
		spacer,
		icon.Color(color),
		pango.Textf(" %d%%", int(info.Remaining()*100)).Color(color),
		spacer,
	}

	if info.PluggedIn() {
		charge := pango.Icon("fa-bolt")
		charge.Color(yellow)
		nodes = append(nodes, charge, spacer)
	}

	out := outputs.Pango(nodes...)

	if perc < 0.1 {
		out.Urgent(true)
	}

	return out

}

func renderTime(t time.Time) bar.Output {

	t = t.Local()
	return outputs.Pango(
		spacer,
		pango.Text(t.Format("Jan 2 15:04")).Color(blue),
		spacer,
	)

}

func renderVolume(v volume.Volume) bar.Output {

	curr := v.Pct()
	nodes := []interface{}{spacer}
	for i := 0; i < 10; i++ {
		node := pango.Icon("fa-square")
		if v.Mute {
			node = pango.Icon("fa-minus-square")
		}
		col := rangeColor(i * 10)
		switch {

		// this block should not show any portion of
		// the current volume
		case i*10 > curr+9:
			col = darkGrey

		// the current volume is within the range of this block
		case i*10 > curr:
			t := float64(i*10-curr) / 10
			c1, _ := colorful.MakeColor(col)
			c2, _ := colorful.MakeColor(darkGrey)
			col = c1.BlendLuv(c2, t)

		// above 100% !!
		case curr > 100:
			col = redStrong

		}

		node.Color(col)
		nodes = append(nodes, node)

	}
	nodes = append(nodes, spacer, pango.Textf("%3d%%", int(curr)))
	return outputs.Pango(nodes...)

}

func rangeColor(percent int) color.Color {

	switch {
	case percent >= 80:
		return red
	case percent >= 60:
		return burntOrange
	case percent >= 40:
		return yellow
	case percent >= 20:
		return green
	default:
		return blue
	}

}

func failIfError(err error) {
	if err == nil {
		return
	}
	fmt.Println(err)
	os.Exit(1)
}
