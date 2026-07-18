# Erratum: Middleware Ordering (01-REQ-8.1)

## Spec vs Implementation

The specification (01-REQ-8.1) defines the middleware execution order as:

1. Panic Recovery
2. Request ID
3. Body Size Limit
4. Content-Type Enforcement
5. Security Headers
6. Logging
7. Handler

## Problem

In Echo's onion middleware model, if Body Size Limit (position 3) or
Content-Type Enforcement (position 4) short-circuits by returning an error
without calling `next(c)`, two issues arise:

1. **Logging (position 6)** is never reached — it cannot observe or log
   responses from middleware at positions 3–5. This violates 01-REQ-8.3
   ("the structured log entry always captures the actual final HTTP status
   code, including error codes produced by middleware steps 1–5").

2. **Security Headers (position 5)** is never reached when Body Size Limit
   or Content-Type Enforcement short-circuits. This violates 01-PROP-4
   ("all three security headers are present on every response").

## Implemented Order

The implementation uses the following corrected order:

1. Panic Recovery — outermost; catches panics from everything
2. Request ID — assigns UUID v4 early for logging and responses
3. Security Headers — sets response headers before any short-circuit
4. Logging — wraps error-producing middleware to capture all status codes
5. Body Size Limit — may short-circuit with 413
6. Content-Type Enforcement — may short-circuit with 415

This ensures:
- Security headers appear on every response (01-PROP-4)
- Logging captures all status codes including 413 and 415 (01-REQ-8.3)
- Request ID is available for all downstream middleware and logging

## Identified By

Critical reviewer finding on 01-REQ-8.1 / 01-REQ-8.3 contradiction.
