package main

func main() {
	var f Fetcher = HTTPFetcher{}
	_ = f.Fetch()
}
