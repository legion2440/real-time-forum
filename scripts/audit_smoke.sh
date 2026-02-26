#!/usr/bin/env bash
set -euo pipefail

BASE=${BASE:-http://localhost:8080}
COOKIE=./cookies.txt

rm -f "$COOKIE"

# Register
curl -s -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@example.com","username":"demo","password":"secret"}' | cat

# Login
curl -s -X POST "$BASE/api/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@example.com","password":"secret"}' \
  -c "$COOKIE" | cat

# Categories
curl -s "$BASE/api/categories" | cat

# Create post
curl -s -X POST "$BASE/api/posts" \
  -H "Content-Type: application/json" \
  -b "$COOKIE" \
  -d '{"title":"Hello","body":"First post","categories":[1,2]}' | cat

# List posts
curl -s "$BASE/api/posts" | cat

# React to post id 1
curl -s -X POST "$BASE/api/posts/1/react" \
  -H "Content-Type: application/json" \
  -b "$COOKIE" \
  -d '{"value":1}' | cat

# Comment on post id 1
curl -s -X POST "$BASE/api/posts/1/comments" \
  -H "Content-Type: application/json" \
  -b "$COOKIE" \
  -d '{"body":"Nice post"}' | cat

# React to comment id 1
curl -s -X POST "$BASE/api/comments/1/react" \
  -H "Content-Type: application/json" \
  -b "$COOKIE" \
  -d '{"value":-1}' | cat

# Manual: open two browsers, login in both; verify only one session active.
