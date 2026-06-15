package dto

// ExportFilter represents the filter, sort, and pagination options for server export.
type ExportFilter struct {
	Status     string `json:"status,omitempty"`
	ServerName string `json:"server_name,omitempty"`
	IPv4       string `json:"ipv4,omitempty"`
	Location   string `json:"location,omitempty"`
	OS         string `json:"os,omitempty"`
	SortBy     string `json:"sort_by,omitempty"`
	SortOrder  string `json:"sort_order,omitempty"`
	Page       int    `json:"page,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
}

// ExportRequest accepts both the current flat export payload and the older
// OpenAPI-documented nested "filter" payload.
type ExportRequest struct {
	ExportFilter
	Filter *ExportFilter `json:"filter,omitempty"`
}

// ToFilter merges nested and top-level filters. Top-level fields win when both
// formats are present so new clients can override legacy nested values.
func (r ExportRequest) ToFilter() ExportFilter {
	var out ExportFilter
	if r.Filter != nil {
		out = *r.Filter
	}

	mergeExportFilter(&out, r.ExportFilter)
	return out
}

func mergeExportFilter(dst *ExportFilter, src ExportFilter) {
	if src.Status != "" {
		dst.Status = src.Status
	}
	if src.ServerName != "" {
		dst.ServerName = src.ServerName
	}
	if src.IPv4 != "" {
		dst.IPv4 = src.IPv4
	}
	if src.Location != "" {
		dst.Location = src.Location
	}
	if src.OS != "" {
		dst.OS = src.OS
	}
	if src.SortBy != "" {
		dst.SortBy = src.SortBy
	}
	if src.SortOrder != "" {
		dst.SortOrder = src.SortOrder
	}
	if src.Page != 0 {
		dst.Page = src.Page
	}
	if src.PageSize != 0 {
		dst.PageSize = src.PageSize
	}
}
