package main

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"log"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"time"

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

func getLogLevel() slog.Level {
	level := os.Getenv("LOG_LEVEL")
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo // デフォルトはINFO
	}
}

func main() {
	// ロガーの初期化
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))
	slog.SetDefault(logger)

	if isLambda() {
		lambda.Start(handler)
	} else {
		if err := handler(events.CloudWatchEvent{}); err != nil {
			logger.Error("failed to execute handler", "error", err)
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
	handlerStart := time.Now()

	now := time.Now()
	slog.Debug("loading AWS config")
	configStart := time.Now()
	awsConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(os.Getenv("AWS_REGION")))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	slog.Debug("AWS config loaded", "duration", time.Since(configStart))

	slog.Debug("loading application config", "path", configPath())
	cfgStart := time.Now()
	cfg, err := NewConfigFromFile(awsConfig, configPath())
	if err != nil {
		return err
	}
	slog.Debug("application config loaded", "duration", time.Since(cfgStart))

	slog.Debug("getting forecasts")
	forecastStart := time.Now()
	forecastsPeriod, forecasts, err := getForecasts(&awsConfig, now)
	if err != nil {
		slog.Error("failed to get forecasts", "error", err)
	}
	slog.Debug("forecasts completed", "duration", time.Since(forecastStart))

	slog.Debug("calculating costs")
	costsStart := time.Now()
	costCalculator := NewCostOfTwoDaysAgo(&awsConfig, now)
	costs, err := costCalculator.GetCosts()
	if err != nil {
		return err
	}
	slog.Debug("costs calculation completed", "duration", time.Since(costsStart))

	slog.Debug("rendering cost graph")
	graphStart := time.Now()
	costGraphRenderer := NewCostGraphRenderer(cfg, &awsConfig, now)
	accounts, costsForGraph, err := costGraphRenderer.GetCosts()
	if err != nil {
		return err
	}
	colors, err := generateColors(cfg.Colors)
	if err != nil {
		return err
	}
	graph, err := drawStackedBarChart(costGraphRenderer.Period(), accounts, costsForGraph, colors)
	if err != nil {
		return err
	}
	slog.Debug("cost graph rendering completed", "duration", time.Since(graphStart))

	slog.Debug("rendering text")
	textStart := time.Now()
	text, err := renderText(forecasts, costs, forecastsPeriod, costCalculator.Period())
	if err != nil {
		log.Fatalf("failed to render: %v", err)
		return err
	}
	slog.Debug("text rendering completed", "duration", time.Since(textStart))

	if dryRun() {
		fmt.Println(text)
	} else {
		slog.Debug("posting to Slack")
		slackStart := time.Now()
		err = postToSlack(cfg, text, graph)
		if err != nil {
			log.Fatalf("failed to post to slack: %v", err)
			return err
		}
		slog.Debug("Slack posting completed", "duration", time.Since(slackStart))
	}

	slog.Info("handler completed", "total_duration", time.Since(handlerStart))
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

func drawStackedBarChart(period *types.DateInterval, accounts []organizationTypes.Account, dailyCosts []DailyCosts, colors []color.Color) (*bytes.Buffer, error) {
	p := plot.New()
	p.Title.Text = "AWS Daily Costs (3 months)"
	p.Y.Label.Text = "Costs (USD)"
	p.Y.AutoRescale = true
	p.Legend.Top = true
	p.Legend.Left = false
	p.Legend.XOffs = 200
	p.Add(plotter.NewGrid())

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
	colors = extendColors(colors, len(bars))
	for i := range bars {
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

// extendColors returns a palette with at least n colors. When the given
// palette is too short, distinct colors are generated by stepping the hue
// by the golden angle so that no two accounts share the same color.
func extendColors(colors []color.Color, n int) []color.Color {
	if len(colors) >= n {
		return colors
	}
	extended := make([]color.Color, len(colors), n)
	copy(extended, colors)
	const goldenAngle = 137.50776405
	for i := 0; len(extended) < n; i++ {
		hue := math.Mod(float64(i)*goldenAngle, 360)
		// Alternate saturation/value slightly to separate neighboring hues
		s := 0.55 + 0.2*float64(i%2)
		v := 0.85 - 0.15*float64((i/2)%2)
		extended = append(extended, hsvToRGBA(hue, s, v))
	}
	return extended
}

func hsvToRGBA(h, s, v float64) color.RGBA {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{
		R: uint8(math.Round((r + m) * 255)),
		G: uint8(math.Round((g + m) * 255)),
		B: uint8(math.Round((b + m) * 255)),
		A: 255,
	}
}

func generateColors(colorConfig []string) ([]color.Color, error) {
	colors := []color.Color{}

	if colorConfig == nil {
		return append(plotutil.SoftColors, plotutil.DarkColors...), nil
	}

	for _, hex := range colorConfig {
		if len(hex) != 7 || hex[0] != '#' {
			return nil, fmt.Errorf("invalid hex color format")
		}
		r, err := strconv.ParseUint(hex[1:3], 16, 8)
		if err != nil {
			return nil, err
		}

		g, err := strconv.ParseUint(hex[3:5], 16, 8)
		if err != nil {
			return nil, err
		}

		b, err := strconv.ParseUint(hex[5:7], 16, 8)
		if err != nil {
			return nil, err
		}
		colors = append(colors, color.RGBA{uint8(r), uint8(g), uint8(b), 255})
	}
	return colors, nil
}
