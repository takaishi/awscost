package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	organizationTypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"strconv"
	"time"
)

type CostOfTwoDaysAgo struct {
	awsConfig *aws.Config
	now       time.Time
}

func NewCostOfTwoDaysAgo(awsConfig *aws.Config, now time.Time) *CostOfTwoDaysAgo {
	return &CostOfTwoDaysAgo{awsConfig: awsConfig, now: now}
}

func (c *CostOfTwoDaysAgo) Period() *types.DateInterval {
	start := c.now.AddDate(0, 0, -2).Format("2006-01-02")
	end := c.now.AddDate(0, 0, -1).Format("2006-01-02")
	return &types.DateInterval{
		Start: aws.String(start),
		End:   aws.String(end),
	}
}

func (c *CostOfTwoDaysAgo) GetCosts() ([]Cost, error) {
	svc := costexplorer.NewFromConfig(*c.awsConfig)
	period := c.Period()
	params := &costexplorer.GetCostAndUsageInput{
		Metrics:     []string{UnblendedCost},
		TimePeriod:  period,
		Granularity: types.GranularityDaily,
		GroupBy: []types.GroupDefinition{
			{
				Type: types.GroupDefinitionTypeDimension,
				Key:  aws.String("LINKED_ACCOUNT"),
			},
			{
				Type: types.GroupDefinitionTypeDimension,
				Key:  aws.String("SERVICE"),
			},
		},
	}
	costAndUsage, err := svc.GetCostAndUsage(context.TODO(), params)
	if err != nil {
		return nil, err
	}

	return c.transformToCosts(costAndUsage)
}

func (c *CostOfTwoDaysAgo) transformToCosts(costAndUsage *costexplorer.GetCostAndUsageOutput) ([]Cost, error) {
	// key: account id
	// value: account name
	linkedAccounts := map[string]string{}
	for _, value := range costAndUsage.DimensionValueAttributes {
		linkedAccounts[*value.Value] = value.Attributes["description"]
	}

	costs := []Cost{}
	for _, value := range costAndUsage.ResultsByTime {
		for _, group := range value.Groups {
			accountName := linkedAccounts[group.Keys[0]]
			serviceName := group.Keys[1]
			amount, err := strconv.ParseFloat(*group.Metrics["UnblendedCost"].Amount, 64)
			if err != nil {
				return nil, err
			}
			costs = append(costs, Cost{AccountName: accountName, ServiceName: serviceName, Amount: amount})
		}
	}

	return costs, nil
}

type ForecastsOfCurrentMonth struct {
	awsConfig *aws.Config
	now       time.Time
}

func NewForecastsOfCurrentMonth(awsConfig *aws.Config, now time.Time) *ForecastsOfCurrentMonth {
	return &ForecastsOfCurrentMonth{awsConfig: awsConfig, now: now}
}

func (f *ForecastsOfCurrentMonth) Period() *types.DateInterval {
	return &types.DateInterval{
		Start: aws.String(f.now.Add(time.Hour * 24).Format("2006-01-02")),
		End:   aws.String(time.Date(f.now.Year(), f.now.Month()+1, 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")),
	}
}

func (f *ForecastsOfCurrentMonth) getAccountIds() ([]organizationTypes.Account, error) {
	organizationSvc := organizations.NewFromConfig(*f.awsConfig)
	listAccountOutput, err := organizationSvc.ListAccounts(context.TODO(), &organizations.ListAccountsInput{})
	if err != nil {
		return nil, err
	}
	return listAccountOutput.Accounts, err
}

func (f *ForecastsOfCurrentMonth) GetForecasts() (map[string]float64, error) {
	period := f.Period()

	// If the start date and end date are the same (both is the end of the month), the forecast is not available.
	if *period.Start == *period.End {
		return nil, nil
	}

	forecasts := make(map[string]float64)
	configSvc := costexplorer.NewFromConfig(*f.awsConfig)

	accounts, err := f.getAccountIds()
	if err != nil {
		return nil, err
	}

	for _, account := range accounts {
		params := &costexplorer.GetCostForecastInput{
			Granularity: types.GranularityMonthly,
			Metric:      "UNBLENDED_COST",
			TimePeriod:  period,
			Filter: &types.Expression{
				Dimensions: &types.DimensionValues{
					Key:    "LINKED_ACCOUNT",
					Values: []string{*account.Id},
				},
			},
		}
		costForecast, err := configSvc.GetCostForecast(context.TODO(), params)
		if err != nil {
			fmt.Printf("unable to get cost forecast for %s, %v", *account.Id, err)
		}
		if costForecast != nil {
			amount, err := strconv.ParseFloat(*costForecast.Total.Amount, 64)
			if err != nil {
				return nil, err
			}
			forecasts[*account.Name] = amount
		}
	}
	return forecasts, nil
}

type CostGraphRenderer struct {
	awsConfig *aws.Config
	now       time.Time
}

func NewCostGraphRenderer(awsConfig *aws.Config, now time.Time) *CostGraphRenderer {
	return &CostGraphRenderer{awsConfig: awsConfig, now: now}
}

func (c *CostGraphRenderer) Period() *types.DateInterval {
	start := c.now.AddDate(0, -3, 0).Format("2006-01-02")
	end := c.now.AddDate(0, 0, -1).Format("2006-01-02")
	return &types.DateInterval{
		Start: aws.String(start),
		End:   aws.String(end),
	}
}

func (c *CostGraphRenderer) GetCosts() ([]organizationTypes.Account, []DailyCosts, error) {
	svc := costexplorer.NewFromConfig(*c.awsConfig)
	results := []types.ResultByTime{}
	dimensionValueAttributes := []types.DimensionValuesWithAttributes{}
	var token *string

	for {
		params := &costexplorer.GetCostAndUsageInput{
			Metrics:     []string{UnblendedCost},
			TimePeriod:  c.Period(),
			Granularity: types.GranularityDaily,
			Filter: &types.Expression{
				Not: &types.Expression{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionService,
						Values: []string{"Tax"},
					},
				},
			},
			GroupBy: []types.GroupDefinition{
				{
					Type: types.GroupDefinitionTypeDimension,
					Key:  aws.String("LINKED_ACCOUNT"),
				},
			},
			NextPageToken: token,
		}
		costAndUsage, err := svc.GetCostAndUsage(context.TODO(), params)
		if err != nil {
			return nil, nil, err
		}
		dimensionValueAttributes = append(dimensionValueAttributes, costAndUsage.DimensionValueAttributes...)
		results = append(results, costAndUsage.ResultsByTime...)
		if costAndUsage.NextPageToken == nil {
			break
		}
		token = costAndUsage.NextPageToken
	}

	accounts, err := c.getAccountIds()
	if err != nil {
		return nil, nil, err
	}
	costs, err := c.transformToCosts(dimensionValueAttributes, results)
	if err != nil {
		return nil, nil, err
	}
	return accounts, costs, nil
}

func (c *CostGraphRenderer) getAccountIds() ([]organizationTypes.Account, error) {
	organizationSvc := organizations.NewFromConfig(*c.awsConfig)
	listAccountOutput, err := organizationSvc.ListAccounts(context.TODO(), &organizations.ListAccountsInput{})
	if err != nil {
		return nil, err
	}
	return listAccountOutput.Accounts, err
}

func (c *CostGraphRenderer) transformToCosts(dimensionValueAttributes []types.DimensionValuesWithAttributes, results []types.ResultByTime) ([]DailyCosts, error) {
	linkedAccounts := map[string]string{}
	for _, value := range dimensionValueAttributes {
		linkedAccounts[*value.Value] = value.Attributes["description"]
	}

	costs := []DailyCosts{}
	for _, value := range results {
		parsed, err := time.Parse("2006-01-02", *value.TimePeriod.End)
		if err != nil {
			return nil, err
		}
		c := DailyCosts{Date: &parsed, Costs: []Cost{}}
		for _, group := range value.Groups {
			accountName := linkedAccounts[group.Keys[0]]
			amount, err := strconv.ParseFloat(*group.Metrics["UnblendedCost"].Amount, 64)
			if err != nil {
				return nil, err
			}
			c.Costs = append(c.Costs, Cost{AccountName: accountName, Amount: amount})
		}
		costs = append(costs, c)
	}

	return costs, nil
}
