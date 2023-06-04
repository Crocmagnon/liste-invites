package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/jomei/notionapi"
)

func main() {
	token := os.Getenv("NOTION_TOKEN")
	dbId := notionapi.DatabaseID(os.Getenv("NOTION_DB"))
	client := notionapi.NewClient(notionapi.Token(token))
	ctx := context.Background()
	request := notionapi.DatabaseQueryRequest{
		PageSize: 100,
	}

	hasMore := true
	updates := 0
	wg := sync.WaitGroup{}
	rateLimit := make(chan bool, 15)

	for hasMore {
		res, err := client.Database.Query(ctx, dbId, &request)
		if err != nil {
			fmt.Println("error querying db: ", err)
			return
		}
		fmt.Printf("Got %d results, processing...\n", len(res.Results))
		for i, page := range res.Results {
			i := i
			page := page
			wg.Add(1)
			rateLimit <- true
			go func() {
				pageTitle, err := processPage(ctx, i, page, err, client)
				if err != nil {
					fmt.Printf("err updating page (%v): %w\n", pageTitle, err)
				}
				updates++
				wg.Done()
				<-rateLimit
			}()
		}
		hasMore = res.HasMore
		request.StartCursor = res.NextCursor
	}
	wg.Wait()
	fmt.Println("Finished,", updates)
}

func processPage(ctx context.Context, i int, page notionapi.Page, err error, client *notionapi.Client) (string, error) {
	id := notionapi.PageID(page.ID)
	var present []string
	status := notionapi.StatusProperty{}
	statusChanged := false
	statusBlocked := false
	var pageTitle string
	for name, prop := range page.Properties {
		{
			title, ok := prop.(*notionapi.TitleProperty)
			if ok {
				pageTitle = title.Title[0].PlainText
				fmt.Printf("%d/%d %v...\n", i+1, 100, pageTitle)
				defer fmt.Println(pageTitle, "ok")
				continue
			}
		}
		prop, ok := prop.(*notionapi.CheckboxProperty)
		if !ok {
			continue
		}
		if !prop.Checkbox {
			continue
		}
		switch name {
		case "Mairie":
			present = append(present, "Mairie")
		case "Team montage":
			present = append(present, "Montage")
		case "Vin d'honneur":
			present = append(present, "Vin d'honneur")
		case "Repas":
			present = append(present, "Repas")
		case "Nuit + Brunch":
			present = append(present, "Nuit + Brunch")
		case "Vin d'honneur confirmé", "Repas confirmé":
			status.Status = notionapi.Option{Name: "Confirmé"}
			statusChanged = true
			statusBlocked = true
		}
		if name == "Invit envoyée" && !statusBlocked {
			status.Status = notionapi.Option{Name: "Invité"}
			statusChanged = true
		}
	}
	wantedOrder := map[string]int{"Mairie": 0, "Montage": 1, "Vin d'honneur": 2, "Repas": 3, "Nuit + Brunch": 4}
	sort.Slice(present, func(i, j int) bool {
		valI := present[i]
		valJ := present[j]
		return wantedOrder[valI] < wantedOrder[valJ]
	})
	presentProp := notionapi.MultiSelectProperty{}
	for _, val := range present {
		presentProp.MultiSelect = append(presentProp.MultiSelect, notionapi.Option{Name: val})
	}
	update := notionapi.PageUpdateRequest{
		Properties: map[string]notionapi.Property{
			"Présent à": presentProp,
		},
	}
	if statusChanged {
		update.Properties["Status"] = status
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = client.Page.Update(ctx, id, &update)
	return pageTitle, err
}
