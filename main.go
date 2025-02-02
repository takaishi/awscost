package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	organizationTypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
	"log"
	"os"
	"sort"
	"time"
)

const (
	UnblendedCost = "UnblendedCost"
)

type Cost struct {
	AccountId   string
	AccountName string
	ServiceName string
	Amount      float64
	TimePeriod  string
}

func main() {
	if isLambda() {
		lambda.Start(handler)
	} else {
		if err := handler(events.CloudWatchEvent{}); err != nil {
			log.Printf("failed to execute handler: %v", err)
			os.Exit(1)
		}
	}
}

func configPath() string {
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "config.json"
	}
	return path
}

func isLambda() bool {
	isLambda := false
	if os.Getenv("IS_LAMBDA") != "" {
		isLambda = os.Getenv("IS_LAMBDA") == "true"
	}
	return isLambda
}

func dryRun() bool {
	dryRun := false
	if os.Getenv("DRY_RUN") != "" {
		dryRun = os.Getenv("DRY_RUN") == "true"
	}
	return dryRun
}

func disableForecast() bool {
	disableForecast := false
	if os.Getenv("DISABLE_FORECAST") != "" {
		disableForecast = os.Getenv("DISABLE_FORECAST") == "true"
	}
	return disableForecast
}

type Bar struct {
	AccountName string
	BarChart    plotter.BarChart
}

func handler(ev events.CloudWatchEvent) error {
	now := time.Now()
	awsConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(os.Getenv("AWS_REGION")))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	cfg, err := NewConfigFromFile(awsConfig, configPath())
	if err != nil {
		return err
	}

	forecastsPeriod, forecasts, err := getForecasts(&awsConfig, now)

	costCalculator := NewCostOfTwoDaysAgo(&awsConfig, now)
	costs, err := costCalculator.GetCosts()
	if err != nil {
		return err
	}

	costGraphRenderer := NewCostGraphRenderer(cfg, &awsConfig, now)
	accounts, costsForGraph, err := costGraphRenderer.GetCosts()
	if err != nil {
		return err
	}
	graph, err := drawStackedBarChart(costGraphRenderer.Period(), accounts, costsForGraph)
	if err != nil {
		return err
	}

	text, err := renderText(forecasts, costs, forecastsPeriod, costCalculator.Period())
	if err != nil {
		log.Fatalf("failed to render: %v", err)
		return err
	}

	if dryRun() {
		fmt.Println(text)
	} else {
		err = postToSlack(cfg, text, graph)
		if err != nil {
			log.Fatalf("failed to post to slack: %v", err)
			return err
		}
	}

	return nil
}

func getForecasts(awsConfig *aws.Config, now time.Time) (*types.DateInterval, map[string]float64, error) {
	if !disableForecast() {
		forecastCalculator := NewForecastsOfCurrentMonth(awsConfig, now)
		forecasts, err := forecastCalculator.GetForecasts()
		if err != nil {
			return forecastCalculator.Period(), forecasts, err
		}
		return forecastCalculator.Period(), forecasts, nil
	}
	return nil, nil, nil
}

type DailyCosts struct {
	Date  *time.Time
	Costs []Cost
}

func drawStackedBarChart(period *types.DateInterval, accounts []organizationTypes.Account, dailyCosts []DailyCosts) (*bytes.Buffer, error) {
	p := plot.New()
	p.Title.Text = "AWS Daily Costs (3 months)"
	p.Y.Label.Text = "Costs (USD)"
	p.Y.AutoRescale = true
	p.Legend.Top = true
	p.Legend.Left = false
	p.Legend.XOffs = 200
	p.Add(plotter.NewGrid())

	colors := append(plotutil.SoftColors, plotutil.DarkColors...)

	maxAmount := 0.0
	nominals := []string{}
	costsByAccount := map[string]plotter.Values{}

	for _, dailyCost := range dailyCosts {
		// Calculate max amount
		dailyMax := 0.0
		for _, cost := range dailyCost.Costs {
			dailyMax += cost.Amount
		}
		if dailyMax > maxAmount {
			maxAmount = dailyMax
		}

		// Calculate nominals
		if dailyCost.Date.Day() == 1 || dailyCost.Date.Format("2006-01-02") == *period.End {
			nominals = append(nominals, dailyCost.Date.Format("2006-01-02"))
		} else {
			nominals = append(nominals, "")
		}

		// Calculate costs by account
		for _, cost := range dailyCost.Costs {
			costsByAccount[cost.AccountName] = append(costsByAccount[cost.AccountName], cost.Amount)
		}
	}
	p.Y.Max = maxAmount * 1.5
	p.NominalX(nominals...)

	bars := []Bar{}
	for _, account := range accounts {
		if costsByAccount[*account.Name] != nil && costsByAccount[*account.Name].Len() > 0 {
			bar, err := plotter.NewBarChart(costsByAccount[*account.Name], vg.Points(5))
			if err != nil {
				return nil, err
			}
			bar.LineStyle.Width = vg.Length(0)
			bars = append(bars, Bar{*account.Name, *bar})
		}
	}

	// Sort by the last value (amount) of the bar chart
	sort.SliceStable(bars, func(i, j int) bool {
		ilen := len(bars[i].BarChart.Values) - 1
		jlen := len(bars[j].BarChart.Values) - 1
		return bars[i].BarChart.Values[ilen] < bars[j].BarChart.Values[jlen]
	})

	// Render legends
	l := plot.NewLegend()
	l.Top = true
	l.YOffs = -p.Title.TextStyle.FontExtents().Height
	for i, _ := range bars {
		bars[i].BarChart.Color = colors[i]
		l.Add(bars[i].AccountName, &bars[i].BarChart)
	}
	img := vgimg.New(1000, 300)
	dc := draw.New(img)
	l.Draw(dc)

	// Render bar charts
	for i, _ := range bars {
		if i > 0 {
			bars[i-1].BarChart.StackOn(&bars[i].BarChart)
		}
		p.Add(&bars[i].BarChart)
	}
	r := l.Rectangle(dc)
	legendWidth := r.Max.X - r.Min.X
	dc = draw.Crop(dc, 0, -legendWidth-vg.Millimeter, 0, 0)
	p.Draw(dc)

	// Write charts to file or buffer
	png := vgimg.PngCanvas{Canvas: img}
	if dryRun() {
		w, err := os.Create("./tmp/timeseries.png")
		if err != nil {
			return nil, err
		}
		if _, err = png.WriteTo(w); err != nil {
			return nil, err
		}
		return nil, nil
	} else {
		buffer := bytes.NewBuffer([]byte{})
		_, err := png.WriteTo(buffer)
		if err != nil {
			return nil, err
		}
		return buffer, nil
	}
}
