"""Thin async client for the EEBUS Bridge add-on REST + WebSocket API."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Awaitable, Callable
from typing import Any

import aiohttp

_LOGGER = logging.getLogger(__name__)


class EebusApiError(Exception):
    """Raised when the add-on API returns an error or is unreachable."""


class EebusApiClient:
    """Talks to the eebus-bridge add-on."""

    def __init__(
        self,
        session: aiohttp.ClientSession,
        host: str,
        port: int,
        token: str | None = None,
    ) -> None:
        self._session = session
        self._host = host
        self._port = port
        self._token = token or None

    @property
    def _base(self) -> str:
        return f"http://{self._host}:{self._port}"

    def _headers(self) -> dict[str, str]:
        if self._token:
            return {"Authorization": f"Bearer {self._token}"}
        return {}

    async def health(self) -> dict[str, Any]:
        """Return the add-on health document (status, ski, version)."""
        return await self._get("/api/health")

    async def get_state(self) -> dict[str, Any]:
        """Return the full state snapshot."""
        return await self._get("/api/state")

    async def set_consumption_limit(
        self, value_w: float, active: bool, duration_s: float
    ) -> None:
        await self._post(
            "/api/lpc/limit",
            {"value_w": value_w, "active": active, "duration_s": duration_s},
        )

    async def set_production_limit(
        self, value_w: float, active: bool, duration_s: float
    ) -> None:
        await self._post(
            "/api/lpp/limit",
            {"value_w": value_w, "active": active, "duration_s": duration_s},
        )

    async def set_eg_consumption_target(
        self, value_w: float, duration_s: float | None = None
    ) -> None:
        await self._post(
            "/api/lpc/target", {"value_w": value_w, "duration_s": duration_s}
        )

    async def set_eg_production_target(
        self, value_w: float, duration_s: float | None = None
    ) -> None:
        await self._post(
            "/api/lpp/target", {"value_w": value_w, "duration_s": duration_s}
        )

    async def activate_eg_consumption(self, active: bool) -> None:
        await self._post("/api/lpc/activate", {"active": active})

    async def activate_eg_production(self, active: bool) -> None:
        await self._post("/api/lpp/activate", {"active": active})

    async def set_lpc_failsafe(
        self, power_w: float | None = None, duration_s: float | None = None
    ) -> None:
        await self._post(
            "/api/lpc/failsafe", {"power_w": power_w, "duration_s": duration_s}
        )

    async def set_lpp_failsafe(
        self, power_w: float | None = None, duration_s: float | None = None
    ) -> None:
        await self._post(
            "/api/lpp/failsafe", {"power_w": power_w, "duration_s": duration_s}
        )

    async def set_lpc_nominal_max(self, value_w: float) -> None:
        await self._post("/api/lpc/nominal_max", {"value_w": value_w})

    async def set_lpp_nominal_max(self, value_w: float) -> None:
        await self._post("/api/lpp/nominal_max", {"value_w": value_w})

    async def trust(self, ski: str) -> None:
        await self._post("/api/pairing/trust", {"ski": ski})

    async def forget(self, ski: str) -> None:
        await self._post("/api/pairing/forget", {"ski": ski})

    async def _get(self, path: str) -> dict[str, Any]:
        try:
            async with self._session.get(
                f"{self._base}{path}",
                headers=self._headers(),
                timeout=aiohttp.ClientTimeout(total=15),
            ) as resp:
                data = await resp.json()
                if resp.status >= 400:
                    raise EebusApiError(data.get("error", f"HTTP {resp.status}"))
                return data
        except (aiohttp.ClientError, asyncio.TimeoutError) as err:
            raise EebusApiError(f"GET {path} failed: {err}") from err

    async def _post(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        try:
            async with self._session.post(
                f"{self._base}{path}",
                json=payload,
                headers=self._headers(),
                timeout=aiohttp.ClientTimeout(total=15),
            ) as resp:
                data = await resp.json()
                if resp.status >= 400:
                    raise EebusApiError(data.get("error", f"HTTP {resp.status}"))
                return data
        except (aiohttp.ClientError, asyncio.TimeoutError) as err:
            raise EebusApiError(f"POST {path} failed: {err}") from err

    async def listen(
        self,
        on_state: Callable[[dict[str, Any]], Awaitable[None] | None],
        stop_event: asyncio.Event,
    ) -> None:
        """Connect to the WebSocket and forward state snapshots until stopped.

        Reconnects with backoff on failure. Returns when stop_event is set.
        """
        url = f"ws://{self._host}:{self._port}/api/ws"
        if self._token:
            url += f"?token={self._token}"

        backoff = 1
        while not stop_event.is_set():
            try:
                async with self._session.ws_connect(
                    url, heartbeat=30, timeout=aiohttp.ClientTimeout(total=15)
                ) as ws:
                    _LOGGER.debug("EEBUS WebSocket connected")
                    backoff = 1
                    async for msg in ws:
                        if stop_event.is_set():
                            break
                        if msg.type == aiohttp.WSMsgType.TEXT:
                            result = on_state(msg.json())
                            if asyncio.iscoroutine(result):
                                await result
                        elif msg.type in (
                            aiohttp.WSMsgType.CLOSED,
                            aiohttp.WSMsgType.ERROR,
                        ):
                            break
            except (aiohttp.ClientError, asyncio.TimeoutError) as err:
                _LOGGER.debug("EEBUS WebSocket error: %s", err)

            if stop_event.is_set():
                break
            try:
                await asyncio.wait_for(stop_event.wait(), timeout=backoff)
            except asyncio.TimeoutError:
                pass
            backoff = min(backoff * 2, 30)
