package main

func accountPages() []page {
	var all []page
	all = append(all, authPages()...)
	all = append(all, peoplePages()...)
	all = append(all, settingsPages()...)
	all = append(all, adminPages()...)
	return all
}
