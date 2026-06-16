"use client";

import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  fileApi,
  monitorApi,
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

export function useCreateServer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: serverApi.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["servers"] }),
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
    onSuccess: () => qc.invalidateQueries({ queryKey: ["servers"] }),
  });
}

// ── Import / Export ──
export function useImportServers() {
  return useMutation({ mutationFn: (file: File) => fileApi.importServers(file) });
}

export function useImportJob(jobId: string | null) {
  return useQuery({
    queryKey: ["import-job", jobId],
    queryFn: () => fileApi.importStatus(jobId as string),
    enabled: !!jobId,
    refetchInterval: (query) => {
      const s = query.state.data?.status;
      return s === "pending" || s === "processing" ? 2000 : false;
    },
  });
}

// ── Reports ──
export function useReportSummary(start: string, end: string, enabled: boolean) {
  return useQuery({
    queryKey: ["report-summary", start, end],
    queryFn: () => reportApi.summary(start, end),
    enabled: enabled && !!start && !!end,
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

// ── Monitor ──
export function useMonitorStatus() {
  return useQuery({
    queryKey: ["monitor-status"],
    queryFn: () => monitorApi.status(),
    retry: false,
    refetchInterval: 30000,
  });
}
