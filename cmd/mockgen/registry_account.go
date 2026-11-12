package main

// accountPages lists the account, organization, auth and admin pages; each family has its own file so parallel
// tickets do not edit the same lines.
func accountPages() []page {
	var all []page
	all = append(all, authPages()...)
	all = append(all, peoplePages()...)
	all = append(all, settingsPages()...)
	all = append(all, adminPages()...)
	return all
}
