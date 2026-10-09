package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const name = "nostr-hotpostrank"

const version = "0.0.9"

var revision = "HEAD"

type HotItem struct {
	ID        string
	PubKey    string
	Reactions map[string]struct{}
	Reposts   map[string]struct{}
}

const botListURL = "https://raw.githubusercontent.com/nostr-jp/botlist/main/botlist.txt"

var (
	relays = []string{
		"wss://yabu.me",
	}
	tt bool
)

func fetchBots(ctx context.Context) (map[string]struct{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, botListURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get botlist: %v", resp.Status)
	}

	bots := map[string]struct{}{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		npub := strings.TrimSpace(scanner.Text())
		if npub == "" {
			continue
		}
		_, pub, err := nip19.Decode(npub)
		if err != nil {
			continue
		}
		if pub, ok := pub.(string); ok {
			bots[pub] = struct{}{}
		}
	}
	return bots, scanner.Err()
}

func hasKana(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

// isJapaneseUser は直近の投稿に仮名を含むものがあれば日本語ユーザーとみなす
func isJapaneseUser(ms nostr.MultiStore, pubkey string) bool {
	evs := findEvents(ms, nostr.Filter{
		Kinds:   []int{nostr.KindTextNote},
		Authors: []string{pubkey},
		Limit:   50,
	})
	for _, ev := range evs {
		if hasKana(ev.Content) {
			return true
		}
	}
	return false
}

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

	bots, err := fetchBots(ctx)
	if err != nil {
		log.Println(err)
	}

	// 期間内に仮名を含む投稿をしたユーザーは日本語ユーザーとみなす
	jpUsers := map[string]bool{}
	for _, note := range notes {
		if hasKana(note.Content) {
			jpUsers[note.PubKey] = true
		}
	}

	m := map[string]*HotItem{}
	for _, note := range notes {
		if _, ok := bots[note.PubKey]; ok {
			continue
		}
		m[note.ID] = &HotItem{
			ID:        note.ID,
			PubKey:    note.PubKey,
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

	// 画像 URL だけの投稿などもあるため、期間内に仮名を含む投稿が無い
	// ユーザーは上位候補に限って過去の投稿を遡って判定する
	ranked := []*HotItem{}
	for _, item := range items {
		if len(item.Reposts)+len(item.Reactions) == 0 || len(ranked) >= 10 {
			break
		}
		jp, ok := jpUsers[item.PubKey]
		if !ok {
			jp = isJapaneseUser(ms, item.PubKey)
			jpUsers[item.PubKey] = jp
		}
		if jp {
			ranked = append(ranked, item)
		}
	}
	items = ranked

	if len(items) == 0 {
		return
	}

	ctx = context.TODO()
	postRanks(ctx, ms, os.Getenv("BOT_NSEC"), items)
}
