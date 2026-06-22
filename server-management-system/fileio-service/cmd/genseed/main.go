// Command genseed produces an import-format Excel containing all 10,000 servers
// that the TCP simulator seeds (deployments/docker/postgres/seed_10k_servers.sql).
//
// The column layout matches the import parser exactly (internal/excel/parser.go):
//   server_id, server_name, ipv4, os, cpu_cores, ram_gb, disk_gb, location, description
//
// ipv4 holds the inventory IP (10.x.x.x), NOT the dial host: health-checks dial
// MONITOR_TCP_DIAL_HOST (tcp-simulator) at tcp_port=9000+i, so ipv4 only needs to
// be a valid address. Re-importing this file re-creates any server that was
// deleted; servers that still exist are reported as duplicates and skipped.
//
// Usage: go run ./cmd/genseed -out /path/to/servers_import_10000.xlsx
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/xuri/excelize/v2"
)

func main() {
	out := flag.String("out", "servers_import_10000.xlsx", "output .xlsx path")
	count := flag.Int("count", 10000, "number of servers to generate")
	flag.Parse()

	categories := []string{"web", "db", "cache", "api", "worker", "proxy", "storage", "monitor", "queue", "ml"}
	locations := []string{"DC-HN", "DC-HCM", "DC-DN", "DC-HP", "DC-CT"}
	osList := []string{"Ubuntu 22.04", "Ubuntu 24.04", "CentOS 9", "Debian 12", "RHEL 9"}
	dcOctets := []int{10, 20, 30, 40, 50}

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		log.Fatalf("stream writer: %v", err)
	}
	headerCells := make([]interface{}, len(headers))
	for i, h := range headers {
		headerCells[i] = h
	}
	if err := sw.SetRow("A1", headerCells); err != nil {
		log.Fatalf("write header: %v", err)
	}

	for i := 1; i <= *count; i++ {
		// Mirror the 1-indexed PostgreSQL array math from seed_10k_servers.sql.
		serverID := fmt.Sprintf("SRV-%05d", i)
		serverName := fmt.Sprintf("%s-%04d", categories[i%10], (i-1)/10+1)

		ipv4 := fmt.Sprintf("10.%d.%d.%d", dcOctets[i%5], ((i-1)/200)%50+1, 10+((i-1)%200))
		osName := osList[i%5]
		location := locations[i%5]

		var cpu, ram, disk int
		switch i % 3 {
		case 0:
			cpu, ram, disk = 16, 64, 2000
		case 1:
			cpu, ram, disk = 8, 32, 1000
		default:
			cpu, ram, disk = 4, 16, 500
		}
		desc := fmt.Sprintf("Auto-generated server #%d", i)

		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		row := []interface{}{serverID, serverName, ipv4, osName, cpu, ram, disk, location, desc}
		if err := sw.SetRow(cell, row); err != nil {
			log.Fatalf("write row %d: %v", i, err)
		}
	}

	if err := sw.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}
	if err := f.SaveAs(*out); err != nil {
		log.Fatalf("save: %v", err)
	}
	log.Printf("wrote %d servers to %s", *count, *out)
}
