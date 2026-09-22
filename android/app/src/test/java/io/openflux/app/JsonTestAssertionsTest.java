package io.openflux.app;

import java.math.BigDecimal;
import java.math.BigInteger;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class JsonTestAssertionsTest {
    @Test public void objectKeyOrderDoesNotMatter() throws Exception {
        assertTrue(JsonTestAssertions.semanticEquals(
                new JSONObject("{\"a\":1,\"b\":2}"), new JSONObject("{\"b\":2,\"a\":1}")));
    }

    @Test public void extraOrDifferentKeyDoesNotCompareEqual() throws Exception {
        JSONObject one = new JSONObject("{\"a\":1}");
        JSONObject two = new JSONObject("{\"a\":1,\"b\":2}");
        assertFalse(JsonTestAssertions.semanticEquals(one, two));
        assertFalse(JsonTestAssertions.semanticEquals(two, one));
        assertFalse(JsonTestAssertions.semanticEquals(one, new JSONObject("{\"b\":1}")));
        assertFalse(JsonTestAssertions.semanticEquals(new JSONObject(),
                new JSONObject().put("missingIsNotNull", JSONObject.NULL)));
    }

    @Test public void arrayOrderAndLengthMatter() throws Exception {
        assertFalse(JsonTestAssertions.semanticEquals(new JSONArray("[1,2]"), new JSONArray("[2,1]")));
        assertFalse(JsonTestAssertions.semanticEquals(new JSONArray("[1]"), new JSONArray("[1,2]")));
        assertTrue(JsonTestAssertions.semanticEquals(new JSONArray("[1,2]"), new JSONArray("[1,2]")));
        assertFalse(JsonTestAssertions.semanticEquals(new JSONObject(), new JSONArray()));
    }

    @Test public void nestedObjectsAndArraysCompareRecursively() throws Exception {
        JSONObject original = new JSONObject("{\"nested\":[{\"ok\":true,\"n\":1},null,\"text\"],\"empty\":{}}");
        assertTrue(JsonTestAssertions.semanticEquals(original, new JSONObject(original.toString())));
        assertTrue(JsonTestAssertions.semanticEquals(original,
                new JSONObject("{\"empty\":{},\"nested\":[{\"n\":1.0,\"ok\":true},null,\"text\"]}")));
        assertFalse(JsonTestAssertions.semanticEquals(original,
                new JSONObject("{\"empty\":{},\"nested\":[{\"n\":2,\"ok\":true},null,\"text\"]}")));
    }

    @Test public void jsonNullAndJavaNullAreHandled() throws Exception {
        assertTrue(JsonTestAssertions.semanticEquals(JSONObject.NULL, JSONObject.NULL));
        assertTrue(JsonTestAssertions.semanticEquals(null, null));
        assertTrue(JsonTestAssertions.semanticEquals(null, JSONObject.NULL));
        assertTrue(JsonTestAssertions.semanticEquals(JSONObject.NULL, null));
        assertFalse(JsonTestAssertions.semanticEquals(JSONObject.NULL, "null"));
        assertFalse(JsonTestAssertions.semanticEquals("null", null));
        assertFalse(JsonTestAssertions.semanticEquals(null, false));
        assertTrue(JsonTestAssertions.semanticEquals(new JSONArray("[null]"),
                new JSONArray().put(JSONObject.NULL)));
    }

    @Test public void numericWrapperTypesDoNotMatter() throws Exception {
        Number[] ones = {Byte.valueOf((byte) 1), Short.valueOf((short) 1), Integer.valueOf(1),
                Long.valueOf(1L), Float.valueOf(1.0f), Double.valueOf(1.0),
                new BigInteger("1"), new BigDecimal("1.000")};
        for (Number left : ones) {
            for (Number right : ones) assertTrue(JsonTestAssertions.semanticEquals(left, right));
        }
        assertTrue(JsonTestAssertions.semanticEquals(new JSONObject("{\"n\":1}"),
                new JSONObject().put("n", new BigDecimal("1.00"))));
        assertTrue(JsonTestAssertions.semanticEquals(0.1d, new BigDecimal("0.10")));
        assertFalse(JsonTestAssertions.semanticEquals(new BigInteger("9007199254740993"),
                new BigInteger("9007199254740992")));
        assertFalse(JsonTestAssertions.semanticEquals(1, 2L));
        assertFalse(JsonTestAssertions.semanticEquals(1, "1"));
    }

    @Test public void stringsAndBooleansRetainTheirTypesAndValues() throws Exception {
        assertTrue(JsonTestAssertions.semanticEquals("same", new String("same")));
        assertFalse(JsonTestAssertions.semanticEquals("a", "b"));
        assertTrue(JsonTestAssertions.semanticEquals(true, true));
        assertFalse(JsonTestAssertions.semanticEquals(true, false));
        assertFalse(JsonTestAssertions.semanticEquals(true, "true"));
        assertFalse(JsonTestAssertions.semanticEquals(false, 0));
    }
}
