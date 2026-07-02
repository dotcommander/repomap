package main

type HTTPFetcher struct{}

func (HTTPFetcher) Fetch() string { return "http" }

type FileFetcher struct{}

func (FileFetcher) Fetch() string { return "file" }
