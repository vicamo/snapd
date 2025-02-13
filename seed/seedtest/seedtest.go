// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package seedtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

// SeedSnaps helps creating snaps for a seed.
type SeedSnaps struct {
	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts

	snaps map[string]string
	infos map[string]*snap.Info

	snapRevs map[string]*asserts.SnapRevision
}

// SetupAssertSigning initializes StoreSigning for storeBrandID and Brands.
func (ss *SeedSnaps) SetupAssertSigning(storeBrandID string) {
	ss.StoreSigning = assertstest.NewStoreStack(storeBrandID, nil)
	ss.Brands = assertstest.NewSigningAccounts(ss.StoreSigning)
}

func (ss *SeedSnaps) AssertedSnapID(snapName string) string {
	cleanedName := strings.Replace(snapName, "-", "", -1)
	return (cleanedName + strings.Repeat("id", 16)[len(cleanedName):])
}

func (ss *SeedSnaps) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string, dbs ...*asserts.Database) (*asserts.SnapDeclaration, *asserts.SnapRevision) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	snapName := info.SnapName()

	snapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, files)

	snapID := ss.AssertedSnapID(snapName)
	declA, err := ss.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"publisher-id": developerID,
		"snap-name":    snapName,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	sha3_384, size, err := asserts.SnapFileSHA3_384(snapFile)
	c.Assert(err, IsNil)

	revA, err := ss.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": sha3_384,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       snapID,
		"developer-id":  developerID,
		"snap-revision": revision.String(),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	if !revision.Unset() {
		info.SnapID = snapID
		info.Revision = revision
	}

	for _, db := range dbs {
		err := db.Add(declA)
		c.Assert(err, IsNil)
		err = db.Add(revA)
		c.Assert(err, IsNil)
	}

	if ss.snaps == nil {
		ss.snaps = make(map[string]string)
		ss.infos = make(map[string]*snap.Info)
		ss.snapRevs = make(map[string]*asserts.SnapRevision)
	}

	ss.snaps[snapName] = snapFile
	info.SideInfo.RealName = snapName
	ss.infos[snapName] = info
	snapDecl := declA.(*asserts.SnapDeclaration)
	snapRev := revA.(*asserts.SnapRevision)
	ss.snapRevs[snapName] = snapRev

	return snapDecl, snapRev
}

func (ss *SeedSnaps) AssertedSnap(snapName string) (snapFile string) {
	return ss.snaps[snapName]
}

func (ss *SeedSnaps) AssertedSnapInfo(snapName string) *snap.Info {
	return ss.infos[snapName]
}

func (ss *SeedSnaps) AssertedSnapRevision(snapName string) *asserts.SnapRevision {
	return ss.snapRevs[snapName]
}

// TestingSeed16 helps setting up a populated Core 16/18 testing seed.
type TestingSeed16 struct {
	SeedSnaps

	SeedDir string
}

func (s *TestingSeed16) SnapsDir() string {
	return filepath.Join(s.SeedDir, "snaps")
}

func (s *TestingSeed16) AssertsDir() string {
	return filepath.Join(s.SeedDir, "assertions")
}

func (s *TestingSeed16) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string) (snapFname string, snapDecl *asserts.SnapDeclaration, snapRev *asserts.SnapRevision) {
	decl, rev := s.SeedSnaps.MakeAssertedSnap(c, snapYaml, files, revision, developerID)

	snapFile := s.snaps[decl.SnapName()]

	snapFname = filepath.Base(snapFile)
	targetFile := filepath.Join(s.SnapsDir(), snapFname)
	err := os.Rename(snapFile, targetFile)
	c.Assert(err, IsNil)

	return snapFname, decl, rev
}

func (s *TestingSeed16) MakeModelAssertionChain(brandID, model string, extras ...map[string]interface{}) []asserts.Assertion {
	assertChain := []asserts.Assertion{}
	modelA := s.Brands.Model(brandID, model, extras...)

	assertChain = append(assertChain, s.Brands.Account(modelA.BrandID()))
	assertChain = append(assertChain, s.Brands.AccountKey(modelA.BrandID()))
	assertChain = append(assertChain, modelA)

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	assertChain = append(assertChain, storeAccountKey)
	return assertChain
}

func (s *TestingSeed16) WriteAssertions(fn string, assertions ...asserts.Assertion) {
	fn = filepath.Join(s.AssertsDir(), fn)
	WriteAssertions(fn, assertions...)
}

func WriteAssertions(fn string, assertions ...asserts.Assertion) {
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := asserts.NewEncoder(f)
	for _, a := range assertions {
		err := enc.Encode(a)
		if err != nil {
			panic(err)
		}
	}
}
