package io.openflux.app;

import java.math.BigDecimal;
import java.util.Iterator;
import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

/** Test-local comparison using the Android-compatible org.json API surface. */
final class JsonTestAssertions {
    private JsonTestAssertions() {}

    static boolean semanticEquals(Object expected, Object actual) throws JSONException {
        if (expected == null || expected == JSONObject.NULL) {
            return actual == null || actual == JSONObject.NULL;
        }
        if (actual == null || actual == JSONObject.NULL) return false;
        if (expected instanceof JSONObject && actual instanceof JSONObject) {
            JSONObject left = (JSONObject) expected;
            JSONObject right = (JSONObject) actual;
            if (left.length() != right.length()) return false;
            Iterator<String> keys = left.keys();
            while (keys.hasNext()) {
                String key = keys.next();
                if (!right.has(key) || !semanticEquals(left.get(key), right.get(key))) return false;
            }
            return true;
        }
        if (expected instanceof JSONArray && actual instanceof JSONArray) {
            JSONArray left = (JSONArray) expected;
            JSONArray right = (JSONArray) actual;
            if (left.length() != right.length()) return false;
            for (int i = 0; i < left.length(); i++) {
                if (!semanticEquals(left.get(i), right.get(i))) return false;
            }
            return true;
        }
        if (expected instanceof Number && actual instanceof Number) {
            try {
                return new BigDecimal(expected.toString()).compareTo(new BigDecimal(actual.toString())) == 0;
            } catch (NumberFormatException invalidJsonNumber) {
                return false;
            }
        }
        if (expected instanceof String || expected instanceof Boolean) return expected.equals(actual);
        return false;
    }
}
