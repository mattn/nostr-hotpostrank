package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const name = "nostr-hotpostrank"

const version = "0.0.5"

var revision = "HEAD"

type HotItem struct {
	ID string
	//Event         *nostr.Event
	ReactionCount int
	RepostCount   int
}

var (
	relays = []string{
		"wss://yabu.me",
	}
	tt bool
)

func postRanks(ctx context.Context, ms nostr.MultiStore, nsec string, items []*HotItem) error {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "最近のホットな話題をお知らせします。 #hotpostrank")
	for i, item := range items {
		note, _ := nip19.EncodeNote(item.ID)
		fmt.Fprintf(&buf, "No%d:", i+1)
		if item.RepostCount > 0 {
			fmt.Fprintf(&buf, " %d reposts", item.RepostCount)
		}
		if item.ReactionCount > 0 {
			fmt.Fprintf(&buf, " %d reactions", item.ReactionCount)
		}
		fmt.Fprintf(&buf, "\n  nostr:%s\n", note)
	}

	if tt {
		io.Copy(os.Stdout, &buf)
		return nil
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
	var ver bool
	flag.BoolVar(&ver, "version", false, "show version")
	flag.BoolVar(&tt, "t", false, "test")
	flag.Parse()

	if ver {
		fmt.Println(version)
		os.Exit(0)
	}

	ms := nostr.MultiStore{}
	feedRelays := []string{
		"wss://yabu.me",
	}
	ctx := context.TODO()
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
		for _, e := range ev.Tags {
			if e.Key() != "e" {
				continue
			}
			hi, ok := m[e.Value()]
			if !ok {
				hi = &HotItem{
					ID:          e.Value(),
					RepostCount: 0,
				}
				m[e.Value()] = hi
			}

			switch ev.Kind {
			case nostr.KindRepost:
				hi.RepostCount++
			case nostr.KindReaction:
				hi.ReactionCount++
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
			//	Kinds: []int{nostr.KindTextNote},
			Kinds: []int{nostr.KindReaction, nostr.KindRepost},
			IDs:   []string{item.ID},
		}
		evs, err := ms.QuerySync(context.Background(), filter)
		if err != nil || len(evs) != 1 {
			continue
		}
		item.RepostCount = 0
		item.ReactionCount = 0
		for _, eev := range evs {
			switch eev.Kind {
			case nostr.KindRepost:
				item.RepostCount++
			case nostr.KindReaction:
				item.ReactionCount++
			}
		}
		//items[n].Event = evs[0]
		if n++; n >= 10 {
			break
		}
	}
	items = items[:n]

	ctx = context.TODO()
	postRanks(ctx, ms, os.Getenv("BOT_NSEC"), items)
}
