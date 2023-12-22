package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/olekukonko/tablewriter"
	"log"
	"sort"
	"strings"
	"text/template"
	"time"
)

const Template = `
{{ .Date }}の合計料金: {{ formatAmount .Total }} USD {{ .ForecastOfCurrentMonth }}

アカウント毎の料金:

{{.CodeFence}}
{{ .CostTable }}
{{.CodeFence}}

上位5サービス:

{{.CodeFence}}
{{ .Top5ServiceTable }}
{{.CodeFence}}
`

func renderText(forecasts map[string]float64, costs []Cost, periodForForecasts *types.DateInterval, period *types.DateInterval) (string, error) {
	data, err := templateData(forecasts, costs, periodForForecasts, period)
	if err != nil {
		return "", err
	}

	var funcMap = template.FuncMap{
		"formatAmount": formatAmount,
	}
	tmpl, err := template.New("").Funcs(funcMap).Parse(Template)
	if err != nil {
		return "", err
	}

	b := new(strings.Builder)
	err = tmpl.Execute(b, data)
	if err != nil {
		log.Fatalf("failed to render text: %v", err)
		return "", err
	}
	return b.String(), nil
}

func formatAmount(amount float64) string {
	return fmt.Sprintf("%.2f", amount)
}

type TemplateData struct {
	Date                     string
	Total                    float64
	TotalForecasts           float64
	Forecasts                map[string]float64
	CostsByAccount           []Cost
	CostsByServiceAndAccount []Cost
	CodeFence                string
	TargetForecastMonth      string
}

func (t TemplateData) ForecastOfCurrentMonth() string {
	if disableForecast() {
		return ""
	} else if t.Forecasts == nil {
		return fmt.Sprintf("(通知日は月末なので料金予測はありません)")
	} else {
		return fmt.Sprintf("(%s月の料金予測: %s USD)", t.TargetForecastMonth, formatAmount(t.TotalForecasts))
	}
}

func templateData(forecasts map[string]float64, costs []Cost, periodForForecasts *types.DateInterval, period *types.DateInterval) (*TemplateData, error) {
	var total float64 = 0
	var totalForecast float64 = 0
	amountsByLinkedAccount := map[string]float64{}

	for _, f := range forecasts {
		totalForecast = totalForecast + f
	}

	for _, c := range costs {
		total = total + c.Amount

		if _, ok := amountsByLinkedAccount[c.AccountName]; ok {
			amountsByLinkedAccount[c.AccountName] = amountsByLinkedAccount[c.AccountName] + c.Amount
		} else {
			amountsByLinkedAccount[c.AccountName] = c.Amount
		}
	}

	costsByAccount := []Cost{}
	for k, v := range amountsByLinkedAccount {
		costsByAccount = append(costsByAccount, Cost{AccountName: k, Amount: v})
	}
	sort.Slice(costsByAccount, func(i, j int) bool {
		return costsByAccount[i].Amount > costsByAccount[j].Amount
	})

	costsByServiceAndAccount := []Cost{}
	sort.Slice(costs, func(i, j int) bool {
		return costs[i].Amount > costs[j].Amount
	})

	if len(costs) >= 5 {
		costsByServiceAndAccount = costs[0:5]
	} else {
		costsByServiceAndAccount = costs
	}

	td := &TemplateData{
		Date:                     *period.Start,
		Total:                    total,
		TotalForecasts:           totalForecast,
		Forecasts:                forecasts,
		CostsByAccount:           costsByAccount,
		CostsByServiceAndAccount: costsByServiceAndAccount,
		CodeFence:                "```",
	}
	if periodForForecasts != nil {
		periodForForecastStart, err := time.Parse("2006-01-02", *periodForForecasts.Start)
		if err != nil {
			return nil, err
		}
		td.TargetForecastMonth = periodForForecastStart.Format("1")
	}
	return td, nil
}

func (t TemplateData) CostTable() string {
	if t.Forecasts == nil {
		return t.CostTableWithoutForecast()
	} else {
		return t.CostTableWithForecast()
	}
}

func (t TemplateData) CostTableWithoutForecast() string {
	buf := new(strings.Builder)
	table := tablewriter.NewWriter(buf)
	table.SetHeader([]string{"Account", "Cost(USD)"})
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	data := [][]string{}
	for _, cost := range t.CostsByAccount {
		data = append(data, []string{
			cost.AccountName,
			fmt.Sprintf("%.2f", cost.Amount),
		})
	}
	table.AppendBulk(data)
	table.Render()
	return buf.String()
}

func (t TemplateData) CostTableWithForecast() string {
	buf := new(strings.Builder)
	table := tablewriter.NewWriter(buf)
	table.SetHeader([]string{"Account", "Cost(USD)", "Forecast"})
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	data := [][]string{}
	for _, cost := range t.CostsByAccount {
		data = append(data, []string{
			cost.AccountName,
			fmt.Sprintf("%.2f", cost.Amount),
			fmt.Sprintf("%.2f", t.Forecasts[cost.AccountName]),
		})
	}
	table.AppendBulk(data)
	table.Render()
	return buf.String()
}

func (t TemplateData) Top5ServiceTable() string {
	buf := new(strings.Builder)
	table := tablewriter.NewWriter(buf)
	table.SetHeader([]string{"Account", "Cost(USD)"})
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	data := [][]string{}
	for _, cost := range t.CostsByServiceAndAccount {
		data = append(data, []string{
			cost.ServiceName,
			fmt.Sprintf("%.2f", cost.Amount),
		})
	}
	table.AppendBulk(data)
	table.Render()
	return buf.String()
}
