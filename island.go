package main

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// A half-block cell contains two pixels. Pairs with equal colours share a
// style run, keeping the artwork inexpensive to repaint over a remote TTY.
var islandArt = sync.OnceValue(func() []string {
	pixels := []string{
		"             aa",
		"           aaaabb          *",
		"       * aaaaaabbbb",
		"         bbaabbbbcc",
		"         bbbbbbcccc",
		"          bbbcccccc",
		"     gg     ttccc  gg",
		"   gggggg   tt   gggggg",
		" ggggghgggggttggggghggggg",
		"gggghgggggggggghggggggggggg",
		"ddggggggghgggggggggggggggff",
		"ddddgggggggggggggggggggffff",
		" dddddddgggggggggggfffffff",
		" ddsdddddddgggggffffffrfff",
		"  ssddsddddddddffffrrrrrr",
		"   sssssdsddddsffrrrvrrr",
		"      ssssssdssrrrrrr",
		"        ssvssssrrvr",
		"           sssrr",
		"             sr",
	}
	colors := map[byte]string{' ': panel, 'a': "#C1F4AD", 'b': "#70C99B", 'c': "#357B71", 't': "#BB9278", 'g': "#94D7A4", 'h': "#C5EBAC", 'd': "#997C86", 'f': "#705E78", 's': "#6D7995", 'r': "#495670", 'v': "#91A4BF", '*': gold}
	const width = 28
	var lines []string
	for y := 0; y < len(pixels); y += 2 {
		upper, lower := fit(pixels[y], width), fit(pixels[y+1], width)
		var line strings.Builder
		for x := 0; x < width; {
			top, bottom := upper[x], lower[x]
			end := x + 1
			for end < width && upper[end] == top && lower[end] == bottom {
				end++
			}
			if top == ' ' && bottom == ' ' {
				line.WriteString(strings.Repeat(" ", end-x))
			} else {
				style := lipgloss.NewStyle().Foreground(lipgloss.Color(colors[top])).Background(lipgloss.Color(colors[bottom]))
				line.WriteString(style.Render(strings.Repeat("▀", end-x)))
			}
			x = end
		}
		lines = append(lines, line.String())
	}
	return lines
})
