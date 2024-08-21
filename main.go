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

const version = "0.0.8"

var revision = "HEAD"

type HotItem struct {
	ID        string
	Reactions map[string]struct{}
	Reposts   map[string]struct{}
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
		if len(item.Reposts) > 0 {
			fmt.Fprintf(&buf, " %d reposts", len(item.Reposts))
		}
		if len(item.Reactions) > 0 {
			fmt.Fprintf(&buf, " %d reactions", len(item.Reactions))
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

func findEvents(ms nostr.MultiStore, filter nostr.Filter) []*nostr.Event {
	evs, _ := ms.QuerySync(context.Background(), filter)
	return evs
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
		Kinds: []int{nostr.KindTextNote},
		Since: &timestamp,
	}

	notes, err := ms.QuerySync(context.Background(), filter)
	if err != nil {
		log.Fatal(err)
	}

	m := map[string]*HotItem{}
	for _, note := range notes {
		m[note.ID] = &HotItem{
			ID:        note.ID,
			Reposts:   map[string]struct{}{},
			Reactions: map[string]struct{}{},
		}
	}

	filter = nostr.Filter{
		Kinds: []int{nostr.KindReaction, nostr.KindRepost},
		Since: &timestamp,
	}

	points, err := ms.QuerySync(context.Background(), filter)
	if err != nil {
		log.Fatal(err)
	}

	for _, point := range points {
		for _, e := range point.Tags {
			if e.Key() != "e" {
				continue
			}
			hi, ok := m[e.Value()]
			if !ok {
				continue
			}

			switch point.Kind {
			case nostr.KindRepost:
				hi.Reposts[point.PubKey] = struct{}{}
			case nostr.KindReaction:
				hi.Reactions[point.PubKey] = struct{}{}
			}
		}
	}

	items := []*HotItem{}
	for _, item := range m {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return len(items[i].Reposts)+len(items[i].Reactions) > len(items[j].Reposts)+len(items[j].Reactions)
	})

	if len(items) > 10 {
		items = items[:10]
	}

	if len(items) == 0 {
		return
	}

	ctx = context.TODO()
	postRanks(ctx, ms, os.Getenv("BOT_NSEC"), items)
}
