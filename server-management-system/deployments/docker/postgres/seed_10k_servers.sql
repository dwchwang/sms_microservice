-- ============================================================
-- Seed 10.000 server cho load test với TCP Simulator.
--
-- ipv4 là địa chỉ private để hiển thị/quản trị; Monitor trong Docker dial qua
-- MONITOR_TCP_DIAL_HOST=tcp-simulator, còn tcp_port = 9000 + index chính là
-- cổng mà Simulator lắng nghe cho server đó.
--
-- Sau khi seed phải chạy: server-service rebuild-monitor-cache
-- Nếu không, Redis target projection rỗng và Monitor sẽ bỏ qua mọi round.
-- ============================================================

\c server_db;

DO $$
DECLARE
    i INTEGER;
    categories TEXT[] := ARRAY['web', 'db', 'cache', 'api', 'worker', 'proxy', 'storage', 'monitor', 'queue', 'ml'];
    locations  TEXT[] := ARRAY['DC-HN', 'DC-HCM', 'DC-DN', 'DC-HP', 'DC-CT'];
    os_list    TEXT[] := ARRAY['Ubuntu 22.04', 'Ubuntu 24.04', 'CentOS 9', 'Debian 12', 'RHEL 9'];
    dc_octets  INTEGER[] := ARRAY[10, 20, 30, 40, 50];
    dc_idx     INTEGER;
    rack_idx   INTEGER;
    host_idx   INTEGER;
    ip_addr    TEXT;
BEGIN
    FOR i IN 1..10000 LOOP
        -- Dải IP private theo DC/rack/host, ví dụ: 10.10.1.11, 10.20.3.42.
        dc_idx   := 1 + (i % 5);
        rack_idx := 1 + (((i - 1) / 200) % 50);
        host_idx := 10 + ((i - 1) % 200);
        ip_addr  := '10.' || dc_octets[dc_idx] || '.' || rack_idx || '.' || host_idx;

        INSERT INTO servers
            (server_id, server_name, status, ipv4, tcp_port,
             os, cpu_cores, ram_gb, disk_gb, location, description)
        VALUES (
            'SRV-' || LPAD(i::TEXT, 5, '0'),
            categories[1 + (i % 10)] || '-' || LPAD(((i - 1) / 10 + 1)::TEXT, 4, '0'),
            -- Server chưa được ping lần nào; Monitor sẽ đặt ON/OFF ở round đầu.
            'UNKNOWN',
            ip_addr::INET,
            9000 + i,
            os_list[1 + (i % 5)],
            CASE WHEN i % 3 = 0 THEN 16   WHEN i % 3 = 1 THEN 8    ELSE 4   END,
            CASE WHEN i % 3 = 0 THEN 64   WHEN i % 3 = 1 THEN 32   ELSE 16  END,
            CASE WHEN i % 3 = 0 THEN 2000 WHEN i % 3 = 1 THEN 1000 ELSE 500 END,
            locations[1 + (i % 5)],
            'Auto-generated server #' || i
        )
        -- ux_servers_server_id là unique toàn cục, nên conflict target không có
        -- điều kiện WHERE như bản cũ.
        ON CONFLICT (server_id) DO UPDATE SET
            ipv4       = EXCLUDED.ipv4,
            tcp_port   = EXCLUDED.tcp_port,
            location   = EXCLUDED.location,
            updated_at = NOW();
    END LOOP;
END $$;
