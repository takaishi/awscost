package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"os"
	"reflect"
	"testing"
	"time"
)

func Test_transformToCosts(t *testing.T) {
	type args struct {
		costAndUsage *costexplorer.GetCostAndUsageOutput
	}
	tests := []struct {
		name    string
		args    args
		want    []Cost
		wantErr bool
	}{
		{
			name: "",
			args: args{
				costAndUsage: &costexplorer.GetCostAndUsageOutput{
					DimensionValueAttributes: []types.DimensionValuesWithAttributes{
						{
							Attributes: map[string]string{
								"description": "foo",
							},
							Value: aws.String("123"),
						},
						{
							Attributes: map[string]string{
								"description": "bar",
							},
							Value: aws.String("456"),
						},
					},
					ResultsByTime: []types.ResultByTime{
						{
							Groups: []types.Group{
								{
									Keys: []string{
										"123",
										"svc1",
									},
									Metrics: map[string]types.MetricValue{
										"UnblendedCost": {
											Amount: aws.String("1.1"),
										},
									},
								},
								{
									Keys: []string{
										"456",
										"svc1",
									},
									Metrics: map[string]types.MetricValue{
										"UnblendedCost": {
											Amount: aws.String("3.2"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: []Cost{
				{
					AccountName: "foo",
					ServiceName: "svc1",
					Amount:      1.1,
				},
				{
					AccountName: "bar",
					ServiceName: "svc1",
					Amount:      3.2,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCostOfTwoDaysAgo(nil, time.Now())
			got, err := c.transformToCosts(tt.args.costAndUsage)
			if (err != nil) != tt.wantErr {
				t.Errorf("transformToCosts() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("transformToCosts() got = %v, want %v", got, tt.want)
			}
		})
	}
}
func Test_renderText(t *testing.T) {
	codeFence := "```"
	type args struct {
		forecasts          map[string]float64
		costs              []Cost
		periodForForecasts *types.DateInterval
		period             *types.DateInterval
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "0",
			args: args{
				forecasts: map[string]float64{
					"account_1": 5.1,
					"account_2": 7.7,
				},
				costs: []Cost{
					{
						AccountName: "account_1",
						ServiceName: "service_a",
						Amount:      1.1,
					},
					{
						AccountName: "account_2",
						ServiceName: "service_a",
						Amount:      3.2,
					},
				},
				periodForForecasts: &types.DateInterval{
					Start: aws.String("2022-11-25"),
					End:   aws.String("2022-12-01"),
				},
				period: &types.DateInterval{
					Start: aws.String("2022-11-23"),
					End:   aws.String("2022-11-24"),
				},
			},
			want: fmt.Sprintf(`
2022-11-23の合計料金: 4.30 USD (11月の料金予測: 12.80 USD)

アカウント毎の料金:

%s
   ACCOUNT   COST(USD)  FORECAST  
  account_2       3.20      7.70  
  account_1       1.10      5.10  

%s

上位5サービス:

%s
   ACCOUNT   COST(USD)  
  service_a       3.20  
  service_a       1.10  

%s
`, codeFence, codeFence, codeFence, codeFence),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderText(tt.args.forecasts, tt.args.costs, tt.args.periodForForecasts, tt.args.period)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				err := os.WriteFile(fmt.Sprintf("./tmp/Test_transformToCosts_%s_got.txt", tt.name), []byte(got), 0644)
				if err != nil {
					t.Errorf("error = %v", err)
				}
				err = os.WriteFile(fmt.Sprintf("./tmp/Test_transformToCosts_%s_want.txt", tt.name), []byte(tt.want), 0644)
				if err != nil {
					t.Errorf("error = %v", err)
				}
				t.Errorf("renderText() got = %v, want %v", got, tt.want)
			}
		})
	}
}
