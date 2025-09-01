package discord

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
)

// buildEventEmbed creates a rich embed for an event with optional banner, links,
// and a prelim/main-card breakdown based on scheduled times or order.
func buildEventEmbed(orgTitle, tzName string, loc *time.Location, e *sources.Event) *discordgo.MessageEmbed {
	if e == nil {
		return nil
	}
	title := strings.TrimSpace(e.Name)
	if title == "" {
		title = e.ShortName
	}
	// Description with start summary
	desc := ""
	if t, err := parseAPITime(e.Start); err == nil {
		local := t.In(loc)
		desc = fmt.Sprintf("Starts: %s (%s)", local.Format("Mon Jan 2, 3:04 PM MST"), tzName)
	}

	emb := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s: %s", orgTitle, title),
		Description: desc,
		Color:       0xE74C3C, // a reddish tone
	}
	if u := primaryEventURL(e); u != "" {
		emb.URL = u // make the title clickable to the main event page
	}
	if strings.TrimSpace(e.BannerURL) != "" {
		emb.Image = &discordgo.MessageEmbedImage{URL: e.BannerURL}
	}

	// Links field (if any)
	if len(e.Links) > 0 {
		var b strings.Builder
		for i, l := range e.Links {
			if l.URL == "" {
				continue
			}
			title := l.Title
			if title == "" {
				title = fmt.Sprintf("Link %d", i+1)
			}
			fmt.Fprintf(&b, "[%s](%s)\n", title, l.URL)
		}
		v := strings.TrimSpace(b.String())
		if v != "" {
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{Name: "Links", Value: v})
		}
	}

	// Card breakdown — reverse order within each section.
	if isContenderSeries(e) {
		// Dana White's Contender Series typically has no prelims; show all as Main Card.
		sorted := sortBouts(e.Bouts)
		mains := reverseBouts(sorted)
		if len(mains) > 0 {
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{Name: "Main Card", Value: formatBouts(mains, loc), Inline: false})
		}
	} else {
		mains, prelims := splitCard(e.Bouts)
		mains = reverseBouts(mains)
		prelims = reverseBouts(prelims)
		if len(mains) > 0 {
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{Name: "Main Card", Value: formatBouts(mains, loc), Inline: false})
		}
		if len(prelims) > 0 {
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{Name: "Prelims", Value: formatBouts(prelims, loc), Inline: false})
		}
	}
	return emb
}

func parseScheduledUTC(s string) (time.Time, bool) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	t, err := parseAPITime(s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func splitCard(bouts []sources.Bout) (mainCard, prelims []sources.Bout) {
	if len(bouts) == 0 {
		return nil, nil
	}
	// Sort by scheduled time ascending; unknowns first by stable order
	bs := sortBouts(bouts)

	n := len(bs)
	cutoff := 0
	switch {
	case n >= 10:
		cutoff = n - 6
	case n >= 6:
		cutoff = n - 3
	default:
		cutoff = n // everything main when short card
	}
	if cutoff < 0 {
		cutoff = 0
	}
	prelims = append(prelims, bs[:cutoff]...)
	mainCard = append(mainCard, bs[cutoff:]...)
	return mainCard, prelims
}

func sortBouts(bouts []sources.Bout) []sources.Bout {
	bs := make([]sources.Bout, len(bouts))
	copy(bs, bouts)
	sort.SliceStable(bs, func(i, j int) bool {
		ti, okI := parseScheduledUTC(bs[i].Scheduled)
		tj, okJ := parseScheduledUTC(bs[j].Scheduled)
		if okI && okJ {
			return ti.Before(tj)
		}
		// Unknown scheduled first to keep stable order
		if !okI && okJ {
			return true
		}
		if okI && !okJ {
			return false
		}
		return i < j
	})
	return bs
}

func reverseBouts(in []sources.Bout) []sources.Bout {
	if len(in) == 0 {
		return in
	}
	out := make([]sources.Bout, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func isContenderSeries(e *sources.Event) bool {
	if e == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(e.Name))
	short := strings.ToLower(strings.TrimSpace(e.ShortName))
	return strings.Contains(name, "contender series") || strings.Contains(short, "contender series")
}

// primaryEventURL picks the best event link for the embed title URL.
// Prefers links labeled like event/gamecast/preview when available.
func primaryEventURL(e *sources.Event) string {
	if e == nil || len(e.Links) == 0 {
		return ""
	}
	// First pass: match common event page titles
	for _, l := range e.Links {
		t := strings.ToLower(strings.TrimSpace(l.Title))
		if t == "event page" || t == "gamecast" || strings.Contains(t, "preview") || strings.Contains(t, "event") {
			if strings.TrimSpace(l.URL) != "" {
				return l.URL
			}
		}
	}
	// Fallback: first link
	return e.Links[0].URL
}

func formatBouts(bs []sources.Bout, loc *time.Location) string {
	if len(bs) == 0 {
		return "—"
	}
	lines := make([]string, 0, len(bs))
	for _, b := range bs {
		names := strings.TrimSpace(fmt.Sprintf("%s vs %s", safe(b.RedName), safe(b.BlueName)))
		wc := strings.TrimSpace(b.WeightClass)
		timePart := ""
		if t, ok := parseScheduledUTC(b.Scheduled); ok {
			timePart = t.In(loc).Format("3:04 PM")
		}
		seg := names
		if wc != "" {
			seg += " — " + wc
		}
		if timePart != "" {
			seg += " — " + timePart
		}
		lines = append(lines, seg)
	}
	out := strings.Join(lines, "\n")
	if len(out) > 1024 { // Discord field value limit
		return out[:1021] + "..."
	}
	return out
}

func safe(s string) string {
	return strings.TrimSpace(s)
}
