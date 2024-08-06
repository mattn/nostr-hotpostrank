package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

type HotItem struct {
	ID            string
	Event         *nostr.Event
	ReactionCount int
	RepostCount   int
}

var (
	relays = []string{
		"wss://yabu.me",
	}
)

func postRanks(ctx context.Context, ms nostr.MultiStore, nsec string, items []*HotItem) error {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "Nostr Hot Post Ranking #hotpostrank")
	for i, item := range items {
		note, _ := nip19.EncodeNote(item.Event.ID)
		fmt.Fprintf(&buf, "No%d:", i+1)
		if item.RepostCount > 0 {
			fmt.Fprintf(&buf, " %d reposts", item.RepostCount)
		}
		if item.ReactionCount > 0 {
			fmt.Fprintf(&buf, " %d reactions", item.ReactionCount)
		}
		fmt.Fprintf(&buf, "\n  nostr:%s\n", note)
	}

	eev := nostr.Event{}
	var sk string
	if _, s, err := nip19.Decode(nsec); err == nil {
		sk = s.(string)
	} else {
		return err
	}
	if pub, err := nostr.GetPublicKey(sk); err == nil {
		if _, err := nip19.EncodePublicKey(pub); err != nil {
			return err
		}
		eev.PubKey = pub
	} else {
		return err
	}

	eev.Content = buf.String()
	eev.CreatedAt = nostr.Now()
	eev.Kind = nostr.KindTextNote
	eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"t", "hotpostrank"})
	eev.Sign(sk)

	return ms.Publish(ctx, eev)
}

func main() {
	ctx := context.Background()

	ms := nostr.MultiStore{}
	feedRelays := []string{
		"wss://yabu.me",
	}
	for _, r := range feedRelays {
		rr, err := nostr.RelayConnect(ctx, r)
		if err == nil {
			ms = append(ms, rr)
		}
	}
	timestamp := nostr.Timestamp(time.Now().Add(-3 * time.Hour).Unix())
	filter := nostr.Filter{
		Kinds: []int{nostr.KindReaction, nostr.KindRepost},
		Since: &timestamp,
	}

	evs, err := ms.QuerySync(context.Background(), filter)
	if err != nil {
		log.Fatal(err)
	}

	m := map[string]*HotItem{}
	for _, ev := range evs {
		es := ev.Tags.GetAll([]string{"e"})
		for _, e := range es {
			if e.Key() != "e" {
				continue
			}
			if hi, ok := m[e.Value()]; ok {
				switch ev.Kind {
				case nostr.KindRepost:
					hi.RepostCount++
				case nostr.KindReaction:
					hi.ReactionCount++
				}
			} else {
				switch ev.Kind {
				case nostr.KindRepost:
					m[e.Value()] = &HotItem{
						ID:          e.Value(),
						RepostCount: 1,
					}
				case nostr.KindReaction:
					m[e.Value()] = &HotItem{
						ID:            e.Value(),
						ReactionCount: 1,
					}
				}
			}
		}
	}

	items := []*HotItem{}
	for _, item := range m {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].RepostCount+items[i].ReactionCount > items[j].RepostCount+items[j].ReactionCount
	})

	n := 0
	for _, item := range items {
		filter := nostr.Filter{
			Kinds: []int{nostr.KindTextNote},
			IDs:   []string{item.ID},
		}
		evs, err := ms.QuerySync(context.Background(), filter)
		if err != nil || len(evs) != 1 {
			continue
		}
		items[n].Event = evs[0]
		if n++; n >= 10 {
			break
		}
	}
	items = items[:n]

	postRanks(ctx, ms, os.Getenv("BOT_NSEC"), items)
}
