package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/guregu/dynamo"
	"github.com/k0kubun/pp"
)

const (
	TypeSale  = 1
	TypeFlyer = 2
)

var (
	debug = flag.Bool("debug", false, "run debug mode")

	TargetStoreURLs = map[string]string{
		"文化堂 緑が丘店":   "https://tokubai.co.jp/文化堂/4111",
		"東急ストア 大岡山店": "https://tokubai.co.jp/東急ストア/5800",
	}
)

type Flyer struct {
	ID        string `dynamo:"id"`
	StoreName string `dynamo:"store_name"`
	StoreURL  string `dynamo:"store_url"`
	Type      int    `dynamo:"type"`
	URL       string `dynamo:"url"`
}

type SlackPayload struct {
	Channel  string   `json:"channel"`
	UserName string   `json:"username"`
	Blocks   []*Block `json:"blocks"`
	Text     string   `json:"text"`
	Markdown bool     `json:"mrkdwn"`
}

type Block struct {
	Type string `json:"type"`
	Text *Text  `json:"text"`
}

type Text struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func Handler(ctx context.Context) error {
	fmt.Println("START: flyer-notify-lambda")

	var flyers []*Flyer

	for sname, turl := range TargetStoreURLs {
		doc, err := goquery.NewDocument(turl)
		if err != nil {
			fmt.Printf("WARNING: %v\n", err)
			continue
		}

		// 特売情報を取得
		doc.Find(".masonry_card_wrapper.product_card_wrapper .masonry_card > a").Each(func(i int, s *goquery.Selection) {
			spath, _ := s.Attr("href")
			u, _ := url.Parse(turl)
			u.Path = path.Join(spath)

			id, _ := s.Attr("id")

			flyers = append(flyers, &Flyer{
				ID:        id,
				StoreName: sname,
				StoreURL:  turl,
				Type:      TypeSale,
				URL:       u.String(),
			})
		})

		// チラシ情報を取得
		doc.Find(".masonry_card_wrapper.leaflet_card > a").Each(func(i int, s *goquery.Selection) {
			fpath, _ := s.Attr("href")
			u, _ := url.Parse(turl)
			u.Path = path.Join(fpath)

			sp := strings.Split(fpath, "/")

			flyers = append(flyers, &Flyer{
				ID:        sp[len(sp)-1],
				StoreName: sname,
				StoreURL:  turl,
				Type:      TypeFlyer,
				URL:       u.String(),
			})
		})
	}

	pp.Println(flyers)

	db := dynamo.New(session.New(), &aws.Config{Region: aws.String(os.Getenv("AWS_DYNAMODB_REGION"))})
	table := db.Table(os.Getenv("DYNAMODB_TABLE"))

	// TODO: とりあえず更新があった場合に、内容は省いた更新通知だけする
	// 必要そうなら内容も含めて通知するように変更する

	sendFlyerMap := map[string]string{}
	for _, flyer := range flyers {
		var tempFS []*Flyer
		if err := table.Scan().Filter("id = ?", flyer.ID).All(&tempFS); err != nil {
			return err
		}

		if len(tempFS) == 0 {
			sendFlyerMap[flyer.StoreName] = flyer.StoreURL

			if err := table.Put(flyer).Run(); err != nil {
				return err
			}
		}
	}

	for sname, surl := range sendFlyerMap {
		if err := PostSlack(&SlackPayload{
			Channel:  os.Getenv("SLACK_CHANNEL"),
			UserName: os.Getenv("SLACK_USER_NAME"),
			Blocks: []*Block{
				{
					Type: "section",
					Text: &Text{
						Type: "mrkdwn",
						Text: fmt.Sprintf("%sの特売チラシが更新されたぞ！\n%s", sname, surl),
					},
				},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func PostSlack(payload *SlackPayload) error {
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, os.Getenv("SLACK_WEBHOOK_URL"), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	bb, err := ioutil.ReadAll(res.Body)
	log.Printf("%+v\n", string(bb))
	defer res.Body.Close()
	return nil
}

func main() {
	flag.Parse()
	if *debug {
		if err := Handler(context.Background()); err != nil {
			panic(err)
		}
	} else {
		lambda.Start(Handler)
	}
}
