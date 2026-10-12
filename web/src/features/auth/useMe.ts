import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@/api/client";

// The shell calls GET /api/me once per mount. No retries: a 401 means "not signed in".
export function useMe() {
  return useQuery({
    queryKey: ["me"],
    retry: false,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/me");
      if (!response.ok || !data) throw ApiError.fromResponse(response);
      return data;
    },
  });
}
