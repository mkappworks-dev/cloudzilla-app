package seed

import (
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type person struct {
	*model.User
	author service.GitAuthor
}

type org struct {
	*model.Organization
	owners  []*person
	members []*person // owners included
}

func (s *seeder) seedPeople() error {
	admin, err := s.svcs.User.CreateSuperadmin(s.ctx, AdminUsername, AdminEmail, s.opts.Password)
	if err != nil {
		return fmt.Errorf("create %s: %w", AdminUsername, err)
	}
	if err := s.addPerson(admin, "Site Admin"); err != nil {
		return err
	}

	taken := map[string]bool{AdminUsername: true}
	for range s.opts.Users {
		first, last := pick(s.rng, firstNames), pick(s.rng, lastNames)
		base := strings.ToLower(first + "-" + last)
		username := base
		for i := 2; taken[username]; i++ {
			username = fmt.Sprintf("%s%d", base, i)
		}
		taken[username] = true
		u, err := s.svcs.User.Create(s.ctx, username, username+"@example.test", s.opts.Password)
		if err != nil {
			return fmt.Errorf("create %s: %w", username, err)
		}
		if err := s.addPerson(u, first+" "+last); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) addPerson(u *model.User, name string) error {
	err := s.svcs.User.UpdateProfile(s.ctx, u.ID, name, u.Email,
		pick(s.rng, bios), pick(s.rng, companies), pick(s.rng, locations), service.Confirmation{})
	if err != nil {
		return fmt.Errorf("profile for %s: %w", u.Username, err)
	}
	author, err := s.svcs.User.CommitAuthor(s.ctx, u.ID)
	if err != nil {
		return err
	}
	u.Name = name
	s.people = append(s.people, &person{User: u, author: author})
	s.report.Users++
	return nil
}

func displayName(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		words[i] = capitalize(w)
	}
	return strings.Join(words, " ")
}

func (s *seeder) seedOrgs() error {
	admin := s.people[0]
	for i := range s.opts.Orgs {
		name := orgNames[i%len(orgNames)]
		if i >= len(orgNames) {
			name = fmt.Sprintf("%s-%d", name, i/len(orgNames)+1)
		}
		creator := s.person()
		o, err := s.svcs.Org.Create(s.ctx, creator.ID, name, displayName(name), pick(s.rng, orgTaglines))
		if err != nil {
			return fmt.Errorf("create org %s: %w", name, err)
		}
		so := &org{Organization: o, owners: []*person{creator}, members: []*person{creator}}

		joiners := s.sample(s.people, 4+s.rng.IntN(16), creator)
		// The admin belongs to the first two orgs so signing in as admin shows org activity.
		if i < 2 && creator.ID != admin.ID && !containsPerson(joiners, admin) {
			joiners = append(joiners, admin)
		}
		for j, p := range joiners {
			role := model.OrgRoleMember
			if j == 0 && chance(s.rng, 0.5) {
				role = model.OrgRoleOwner
				so.owners = append(so.owners, p)
			}
			if err := s.svcs.Org.AddMember(s.ctx, o.ID, creator.ID, p.ID, role); err != nil {
				return fmt.Errorf("add %s to %s: %w", p.Username, name, err)
			}
			so.members = append(so.members, p)
		}
		s.orgs = append(s.orgs, so)
		s.report.Orgs++
	}
	return nil
}
