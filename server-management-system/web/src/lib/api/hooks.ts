"use client";

import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  fileApi,
  reportApi,
  serverApi,
  userApi,
  type ServerListParams,
} from "./endpoints";

// ── Servers ──
export function useServers(
  params: ServerListParams,
  options?: { refetchInterval?: number },
) {
  return useQuery({
    queryKey: ["servers", params],
    queryFn: () => serverApi.list(params),
    placeholderData: keepPreviousData,
    refetchInterval: options?.refetchInterval,
  });
}

export function useServer(serverId: string) {
  return useQuery({
    queryKey: ["server", serverId],
    queryFn: () => serverApi.get(serverId),
    enabled: !!serverId,
  });
}

// One cached call instead of three list queries.
export function useServerStats(options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ["server-stats"],
    queryFn: () => serverApi.stats(),
    refetchInterval: options?.refetchInterval,
  });
}

// Lifetime uptime from Redis counters — no snapshot, answers immediately.
export function useServerUptime(options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ["server-uptime"],
    queryFn: () => serverApi.uptime(),
    refetchInterval: options?.refetchInterval,
  });
}

export function useCreateServer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: serverApi.create,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["servers"] });
      qc.invalidateQueries({ queryKey: ["server-stats"] });
    },
  });
}

export function useUpdateServer(serverId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Parameters<typeof serverApi.update>[1]) =>
      serverApi.update(serverId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["servers"] });
      qc.invalidateQueries({ queryKey: ["server", serverId] });
    },
  });
}

export function useDeleteServer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (serverId: string) => serverApi.remove(serverId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["servers"] });
      qc.invalidateQueries({ queryKey: ["server-stats"] });
    },
  });
}

// ── Import ──
export function useImportServers() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => fileApi.importServers(file),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["servers"] });
      qc.invalidateQueries({ queryKey: ["server-stats"] });
    },
  });
}

// ── Reports ──
export function useReportSummary(start: string, end: string, enabled: boolean) {
  return useQuery({
    queryKey: ["report-summary", start, end],
    queryFn: () => reportApi.summary(start, end),
    enabled: enabled && !!start && !!end,
    retry: false,
  });
}

export function useSendReport() {
  return useMutation({ mutationFn: reportApi.send });
}

// ── Users ──
export function useUsers(page: number) {
  return useQuery({
    queryKey: ["users", page],
    queryFn: () => userApi.list(page),
    placeholderData: keepPreviousData,
  });
}

export function useUpdateUserRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: string }) =>
      userApi.updateRole(userId, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}
