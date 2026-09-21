package io.openflux.app;

import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class ReturnPathStatusTest {
    @Test public void optimisticConnectedIsNotCarrierReadiness() throws Exception {
        JSONObject d = new JSONObject();
        d.put("connected", true);
        d.put("transport", "vyandex");
        assertEquals("transport_not_started", ReturnPathStatus.describe(d));
        d.put("transport_started", true);
        assertEquals("transport_started_waiting_for_ws", ReturnPathStatus.describe(d));
        d.put("ws_connected", true);
        assertEquals("ws_open_no_usable_return_packet", ReturnPathStatus.describe(d));
        d.put("mobile_callback_packets", 1);
        assertEquals("return_packets_observed_internet_not_verified", ReturnPathStatus.describe(d));
        d.put("ws_connected", false);
        assertEquals("transport_started_waiting_for_ws", ReturnPathStatus.describe(d));
    }
}
