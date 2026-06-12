-- ============================================================
-- Seed 10.000 Servers cho TCP Simulator
-- Tất cả server có ipv4 = 'tcp-simulator' và tcp_port = 9000 + index
-- ============================================================

DO $$
DECLARE
    i INTEGER;
    uptime DECIMAL(3,2);
    categories TEXT[] := ARRAY['web', 'db', 'cache', 'api', 'worker', 'proxy', 'storage', 'monitor', 'queue', 'ml'];
    locations TEXT[] := ARRAY['DC-HN', 'DC-HCM', 'DC-DN', 'DC-HP', 'DC-CT'];
    os_list TEXT[] := ARRAY['Ubuntu 22.04', 'Ubuntu 24.04', 'CentOS 9', 'Debian 12', 'RHEL 9'];
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

        -- Insert server
        INSERT INTO server_schema.servers
            (server_id, server_name, status, ipv4, os, cpu_cores, ram_gb, disk_gb, location, description)
        VALUES (
            'SRV-' || LPAD(i::TEXT, 5, '0'),
            categories[1 + (i % 10)] || '-' || LPAD(((i-1)/10 + 1)::TEXT, 4, '0'),
            'off',
            'tcp-simulator',
            os_list[1 + (i % 5)],
            CASE WHEN i % 3 = 0 THEN 16 WHEN i % 3 = 1 THEN 8 ELSE 4 END,
            CASE WHEN i % 3 = 0 THEN 64 WHEN i % 3 = 1 THEN 32 ELSE 16 END,
            CASE WHEN i % 3 = 0 THEN 2000 WHEN i % 3 = 1 THEN 1000 ELSE 500 END,
            locations[1 + (i % 5)],
            'Auto-generated server #' || i
        )
        ON CONFLICT (server_id) DO NOTHING;

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
        ON CONFLICT (server_id) DO NOTHING;
    END LOOP;
END $$;
