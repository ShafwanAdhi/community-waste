package domain

type WasteSummary struct {
	Type   WasteType    `json:"type"`
	Status PickupStatus `json:"status"`
	Count  int64        `json:"count"`
}

type PaymentStatusSummary struct {
	Status PaymentStatus `json:"status"`
	Count  int64         `json:"count"`
	Amount Money         `json:"amount"`
}

type PaymentSummary struct {
	Statuses     []PaymentStatusSummary `json:"statuses"`
	TotalRevenue Money                  `json:"total_revenue"`
}
