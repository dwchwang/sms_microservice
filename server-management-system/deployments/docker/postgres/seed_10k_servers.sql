-- ============================================================
-- Seed 10.000 Servers cho TCP Simulator
-- Server dùng IPv4 private thực tế để hiển thị/quản trị.
-- Monitor trong Docker dial qua MONITOR_TCP_DIAL_HOST=tcp-simulator và tcp_port = 9000 + index.
-- ============================================================

DO $$
DECLARE
    i INTEGER;
    uptime DECIMAL(3,2);
    categories TEXT[] := ARRAY['web', 'db', 'cache', 'api', 'worker', 'proxy', 'storage', 'monitor', 'queue', 'ml'];
    locations TEXT[] := ARRAY['DC-HN', 'DC-HCM', 'DC-DN', 'DC-HP', 'DC-CT'];
    os_list TEXT[] := ARRAY['Ubuntu 22.04', 'Ubuntu 24.04', 'CentOS 9', 'Debian 12', 'RHEL 9'];
    dc_octets INTEGER[] := ARRAY[10, 20, 30, 40, 50];
    dc_idx INTEGER;
    rack_idx INTEGER;
    host_idx INTEGER;
    ip_addr TEXT;
BEGIN
    FOR i IN 1..10000 LOOP
        -- Uptime rate phân bố đa dạng:
        -- 70% servers: uptime 0.93-0.99 (tốt)
        -- 20% servers: uptime 0.80-0.93 (trung bình)
        -- 10% servers: uptime 0.50-0.80 (kém)
        IF i % 10 = 0 THEN
            uptime := 0.50 + (random() * 0.30);          -- 50-80%
        ELSIF i % 5 = 0 THEN
            uptime := 0.80 + (random() * 0.13);          -- 80-93%
        ELSE
            uptime := 0.93 + (random() * 0.06);          -- 93-99%
        END IF;

        -- Dải IP private theo DC/rack/host, ví dụ: 10.10.1.11, 10.20.3.42.
        dc_idx := 1 + (i % 5);
        rack_idx := 1 + (((i - 1) / 200) % 50);
        host_idx := 10 + ((i - 1) % 200);
        ip_addr := '10.' || dc_octets[dc_idx] || '.' || rack_idx || '.' || host_idx;

        -- Insert server
        INSERT INTO server_schema.servers
            (server_id, server_name, status, ipv4, os, cpu_cores, ram_gb, disk_gb, location, description)
        VALUES (
            'SRV-' || LPAD(i::TEXT, 5, '0'),
            categories[1 + (i % 10)] || '-' || LPAD(((i-1)/10 + 1)::TEXT, 4, '0'),
            'off',
            ip_addr,
            os_list[1 + (i % 5)],
            CASE WHEN i % 3 = 0 THEN 16 WHEN i % 3 = 1 THEN 8 ELSE 4 END,
            CASE WHEN i % 3 = 0 THEN 64 WHEN i % 3 = 1 THEN 32 ELSE 16 END,
            CASE WHEN i % 3 = 0 THEN 2000 WHEN i % 3 = 1 THEN 1000 ELSE 500 END,
            locations[1 + (i % 5)],
            'Auto-generated server #' || i
        )
        ON CONFLICT (server_id) WHERE deleted_at IS NULL DO UPDATE SET
            ipv4 = EXCLUDED.ipv4,
            location = EXCLUDED.location,
            updated_at = NOW();

        -- Insert health check config
        INSERT INTO monitor_schema.health_check_configs
            (server_id, check_method, tcp_port, tcp_timeout_ms, uptime_rate)
        VALUES (
            'SRV-' || LPAD(i::TEXT, 5, '0'),
            'tcp',
            9000 + i,
            5000,
            uptime
        )
        ON CONFLICT (server_id) DO UPDATE SET
            tcp_port = EXCLUDED.tcp_port,
            tcp_timeout_ms = EXCLUDED.tcp_timeout_ms,
            uptime_rate = EXCLUDED.uptime_rate,
            updated_at = NOW();
    END LOOP;
END $$;
