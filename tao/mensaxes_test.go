package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// loginAs spins up a fake File Browser seeded with the shared folder and logs the
// app in as user, returning the ready App and the fake so tests can inspect files.
func loginAs(t *testing.T, user string, prof bool, seed func(*fakeFileBrowser)) (*App, *fakeFileBrowser) {
	t.Helper()
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	if prof {
		fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{user})
	}
	if seed != nil {
		seed(fake)
	}
	srv := httptest.NewServer(fake.handler(user, "segredo"))
	t.Cleanup(srv.Close)

	a := NewApp()
	a.dataDir = t.TempDir()
	if _, err := a.loginServer(user, "segredo", srv.URL); err != nil {
		t.Fatalf("loginServer(%s): %v", user, err)
	}
	return a, fake
}

func TestStudentAndProfessorExchangeMessages(t *testing.T) {
	// The professor and the student share the SAME fake server, so a message one
	// writes the other can read. Use one fake, two App sessions.
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/profesores.json"] = mustJSON(t, []string{"prof"})
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"alumno": {Name: "Ana", Surname: "Pérez", Group: "eso4a"},
	})

	studentSrv := httptest.NewServer(fake.handler("alumno", "segredo"))
	defer studentSrv.Close()
	profSrv := httptest.NewServer(fake.handler("prof", "segredo"))
	defer profSrv.Close()

	student := NewApp()
	student.dataDir = t.TempDir()
	if _, err := student.loginServer("alumno", "segredo", studentSrv.URL); err != nil {
		t.Fatalf("login alumno: %v", err)
	}
	prof := NewApp()
	prof.dataDir = t.TempDir()
	if _, err := prof.loginServer("prof", "segredo", profSrv.URL); err != nil {
		t.Fatalf("login prof: %v", err)
	}

	// Student → professor.
	if err := student.SendMessageToProfessor("mat1", "Ola, teño unha dúbida", nil); err != nil {
		t.Fatalf("SendMessageToProfessor: %v", err)
	}
	got, err := prof.GetStudentMessages("alumno", "mat1")
	if err != nil {
		t.Fatalf("GetStudentMessages: %v", err)
	}
	if len(got) != 1 || got[0].From != "alumno" || got[0].Text != "Ola, teño unha dúbida" {
		t.Fatalf("o profesor non ve a mensaxe do alumno: %+v", got)
	}
	if got[0].Author != "alumno" || got[0].AuthorName != "Ana Pérez" {
		t.Fatalf("autoría inesperada: %+v", got[0])
	}

	// Guarantee the reply lands in a later millisecond so the strict ordering below is
	// meaningful (in real use two people rarely send within the same tick, and readThread
	// tie-breaks on the id anyway).
	time.Sleep(2 * time.Millisecond)

	// Professor → student, same materia.
	if err := prof.SendMessageToStudent("alumno", "mat1", "Dime cal é", nil); err != nil {
		t.Fatalf("SendMessageToStudent: %v", err)
	}
	mine, err := student.GetMyMessages("mat1")
	if err != nil {
		t.Fatalf("GetMyMessages: %v", err)
	}
	if len(mine) != 2 {
		t.Fatalf("o alumno debería ver 2 mensaxes, ve %d: %+v", len(mine), mine)
	}
	// Ordered oldest-first: the student's message, then the professor's reply.
	if mine[0].From != "alumno" || mine[1].From != "profesor" || mine[1].Text != "Dime cal é" {
		t.Fatalf("orde/contido inesperado: %+v", mine)
	}
}

func TestMessagesAreScopedPerMateria(t *testing.T) {
	a, _ := loginAs(t, "alumno", false, func(f *fakeFileBrowser) {
		f.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
			"alumno": {Group: "eso4a"},
		})
	})
	if err := a.SendMessageToProfessor("mat1", "de mat1", nil); err != nil {
		t.Fatalf("send mat1: %v", err)
	}
	if err := a.SendMessageToProfessor("mat2", "de mat2", nil); err != nil {
		t.Fatalf("send mat2: %v", err)
	}
	m1, _ := a.GetMyMessages("mat1")
	m2, _ := a.GetMyMessages("mat2")
	if len(m1) != 1 || m1[0].Text != "de mat1" {
		t.Fatalf("mat1 debería ter só a súa mensaxe: %+v", m1)
	}
	if len(m2) != 1 || m2[0].Text != "de mat2" {
		t.Fatalf("mat2 debería ter só a súa mensaxe: %+v", m2)
	}
}

func TestBroadcastToMateriaReachesGroupOnly(t *testing.T) {
	prof, fake := loginAs(t, "prof", true, func(f *fakeFileBrowser) {
		f.files["dixitalizacion/materias/mat1.json"] = mustJSON(t, MateriaDef{
			ID: "mat1", Name: "Tec", Groups: []string{"eso4a"},
		})
		f.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
			"ana":  {Name: "Ana", Group: "eso4a"},
			"beto": {Name: "Beto", Group: "eso4a"},
			"cora": {Name: "Cora", Group: "bac2"}, // outro grupo: non debe recibir
		})
	})

	n, err := prof.BroadcastToMateria("mat1", "Mañá exame")
	if err != nil {
		t.Fatalf("BroadcastToMateria: %v", err)
	}
	if n != 2 {
		t.Fatalf("difusión debería chegar a 2 alumnos, chegou a %d", n)
	}
	for _, u := range []string{"ana", "beto"} {
		found := false
		for p := range fake.files {
			if strings.HasPrefix(p, "users/"+u+"/mensaxes/mat1/") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s non recibiu a difusión; ficheiros: %v", u, keys(fake.files))
		}
	}
	for p := range fake.files {
		if strings.HasPrefix(p, "users/cora/mensaxes/") {
			t.Fatalf("cora (bac2) non debería recibir a difusión de eso4a: %s", p)
		}
	}
}

func TestSendMessageToStudentsReachesSubset(t *testing.T) {
	prof, fake := loginAs(t, "prof", true, func(f *fakeFileBrowser) {
		f.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
			"ana":  {Group: "eso4a"},
			"beto": {Group: "eso4a"},
			"cora": {Group: "eso4a"},
		})
	})

	// Send to two of the three, with a duplicate that must be collapsed.
	n, err := prof.SendMessageToStudents([]string{"ana", "beto", "ana", "  "}, "mat1", "só a vós dous", nil)
	if err != nil {
		t.Fatalf("SendMessageToStudents: %v", err)
	}
	if n != 2 {
		t.Fatalf("esperábanse 2 destinatarios, obtívose %d", n)
	}
	if _, ok := fake.files["users/cora/mensaxes/mat1"]; ok {
		t.Fatal("cora non debería recibir a mensaxe")
	}
	hasThread := func(u string) bool {
		for p := range fake.files {
			if strings.HasPrefix(p, "users/"+u+"/mensaxes/mat1/") {
				return true
			}
		}
		return false
	}
	if !hasThread("ana") || !hasThread("beto") {
		t.Fatalf("ana e beto deberían ter a mensaxe; ficheiros: %v", keys(fake.files))
	}
	// A message to several is flagged as a group notice.
	got, _ := prof.GetStudentMessages("ana", "mat1")
	if len(got) != 1 || !got[0].Broadcast {
		t.Fatalf("unha mensaxe a varios debería marcarse como difusión: %+v", got)
	}

	// A single recipient is a personal message (not flagged).
	if _, err := prof.SendMessageToStudents([]string{"cora"}, "mat1", "só ti", nil); err != nil {
		t.Fatalf("SendMessageToStudents (un): %v", err)
	}
	solo, _ := prof.GetStudentMessages("cora", "mat1")
	if len(solo) != 1 || solo[0].Broadcast {
		t.Fatalf("unha mensaxe a un só non debería ser difusión: %+v", solo)
	}

	// Empty recipients is an error.
	if _, err := prof.SendMessageToStudents(nil, "mat1", "ninguén", nil); err == nil {
		t.Fatal("unha lista baleira de destinatarios debería fallar")
	}
}

func TestMessagingGuardsRequireProfessor(t *testing.T) {
	a, _ := loginAs(t, "alumno", false, nil)
	if _, err := a.GetStudentMessages("outro", "mat1"); err == nil {
		t.Fatal("un alumno non debería ler o fío doutro alumno")
	}
	if err := a.SendMessageToStudent("outro", "mat1", "hola", nil); err == nil {
		t.Fatal("un alumno non debería escribir no fío doutro alumno")
	}
	if _, err := a.BroadcastToMateria("mat1", "hola"); err == nil {
		t.Fatal("un alumno non debería poder difundir")
	}
	if _, err := a.SendMessageToStudents([]string{"outro"}, "mat1", "hola", nil); err == nil {
		t.Fatal("un alumno non debería poder enviar a outros alumnos")
	}
}

func TestMessageValidation(t *testing.T) {
	a, _ := loginAs(t, "alumno", false, nil)
	if err := a.SendMessageToProfessor("mat1", "   ", nil); err == nil {
		t.Fatal("unha mensaxe baleira debería rexeitarse")
	}
	if err := a.SendMessageToProfessor("", "hola", nil); err == nil {
		t.Fatal("unha materia baleira debería rexeitarse")
	}
	if err := a.SendMessageToProfessor("../escape", "hola", nil); err == nil {
		t.Fatal("un id de materia con / debería rexeitarse")
	}
}

// seedGroup returns a fake with two students of the same group in the roster.
func seedGroup(t *testing.T) *fakeFileBrowser {
	t.Helper()
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana":  {Name: "Ana", Group: "eso4a"},
		"beto": {Name: "Beto", Group: "eso4a"},
		"cora": {Name: "Cora", Group: "bac2"},
	})
	return fake
}

func TestGroupWallSharedAmongGroup(t *testing.T) {
	fake := seedGroup(t)
	ana := loginStudent(t, fake, "ana")
	beto := loginStudent(t, fake, "beto")

	if err := ana.PostToWall("eso4a", "Mañá exame", nil); err != nil {
		t.Fatalf("PostToWall: %v", err)
	}
	got, err := beto.GetWallMessages("eso4a")
	if err != nil {
		t.Fatalf("GetWallMessages: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Mañá exame" || got[0].Author != "ana" {
		t.Fatalf("o muro non se compartiu no grupo: %+v", got)
	}
	// A student cannot post to another group's wall.
	if err := ana.PostToWall("bac2", "intruso", nil); err == nil {
		t.Fatal("un alumno non debería escribir no muro doutro grupo")
	}
	// The wall lives in the shared folder, keyed by group.
	found := false
	for p := range fake.files {
		if strings.HasPrefix(p, "dixitalizacion/mensaxes/mural/eso4a/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("o muro non se gardou no cartafol compartido; ficheiros: %v", keys(fake.files))
	}
}

func TestPeerMessagingWithinGroup(t *testing.T) {
	fake := seedGroup(t)
	ana := loginStudent(t, fake, "ana")
	beto := loginStudent(t, fake, "beto")

	if err := ana.SendPeerMessage("beto", "Ola Beto", nil); err != nil {
		t.Fatalf("SendPeerMessage: %v", err)
	}
	// Both peers read the same thread (order-independent folder).
	forBeto, err := beto.GetPeerMessages("ana")
	if err != nil {
		t.Fatalf("GetPeerMessages (beto): %v", err)
	}
	if len(forBeto) != 1 || forBeto[0].Text != "Ola Beto" || forBeto[0].Author != "ana" {
		t.Fatalf("beto non ve a mensaxe privada: %+v", forBeto)
	}
	forAna, _ := ana.GetPeerMessages("beto")
	if len(forAna) != 1 {
		t.Fatalf("ana debería ver a mesma conversa: %+v", forAna)
	}
	if err := ana.SendPeerMessage("ana", "a min", nil); err == nil {
		t.Fatal("non debería poder enviarse a si mesma")
	}
	// Cora (bac2) non comparte nin grupo nin materia con ana.
	cora := loginStudent(t, fake, "cora")
	if err := cora.SendPeerMessage("ana", "ola", nil); err == nil {
		t.Fatal("non debería poder falar con alguén doutro grupo e doutra materia")
	}
}

// TestPeerMessagingAcrossGroupsOfSameMateria cobre o caso real: unha materia que
// xunta bac1a e bac1b. Ese alumnado é compañeiro aínda que non comparta grupo.
func TestPeerMessagingAcrossGroupsOfSameMateria(t *testing.T) {
	fake := newFakeFileBrowser("tok")
	seedShared(fake)
	fake.files["dixitalizacion/alumnos.json"] = mustJSON(t, map[string]RosterEntry{
		"ana":  {Name: "Ana", Group: "bac1a"},
		"uxia": {Name: "Uxía", Group: "bac1b"},
		"cora": {Name: "Cora", Group: "eso4a"}, // ningunha materia en común
	})
	fake.files["dixitalizacion/materias/filo.json"] = mustJSON(t, MateriaDef{
		ID: "filo", Name: "Filosofía", Groups: []string{"bac1a", "bac1b"},
	})
	ana := loginStudent(t, fake, "ana")
	uxia := loginStudent(t, fake, "uxia")

	if err := ana.SendPeerMessage("uxia", "Ola Uxía", nil); err != nil {
		t.Fatalf("SendPeerMessage entre grupos da mesma materia: %v", err)
	}
	got, err := uxia.GetPeerMessages("ana")
	if err != nil {
		t.Fatalf("GetPeerMessages (uxia): %v", err)
	}
	if len(got) != 1 || got[0].Text != "Ola Uxía" {
		t.Fatalf("uxía non ve a mensaxe de ana: %+v", got)
	}
	// Uxía ve a ana na súa lista de compañeiros, e cora non aparece.
	peers, err := uxia.ListMyPeers()
	if err != nil {
		t.Fatalf("ListMyPeers: %v", err)
	}
	vistos := map[string]string{}
	for _, p := range peers {
		vistos[p.Username] = p.Group
	}
	if vistos["ana"] != "bac1a" {
		t.Fatalf("ana debería aparecer como compañeira de materia (co seu grupo): %+v", peers)
	}
	if _, ok := vistos["cora"]; ok {
		t.Fatalf("cora non comparte materia: non debería aparecer: %+v", peers)
	}
	// E alguén sen materia común segue sen poder escribir.
	cora := loginStudent(t, fake, "cora")
	if err := cora.SendPeerMessage("ana", "ola", nil); err == nil {
		t.Fatal("cora non comparte materia con ana; non debería poder escribirlle")
	}
}

// TestPeerMessagingReadsLegacyGroupThread comproba que o historial gardado na ruta
// antiga (1a1/<grupo>/<a>__<b>) segue visible despois do cambio de ruta.
func TestPeerMessagingReadsLegacyGroupThread(t *testing.T) {
	fake := seedGroup(t)
	vella := Message{ID: "1000-aaaa", From: "alumno", Author: "beto", Text: "mensaxe vella", TS: 1000}
	fake.dirs["dixitalizacion/mensaxes/1a1/eso4a/ana__beto"] = true
	fake.files["dixitalizacion/mensaxes/1a1/eso4a/ana__beto/1000-aaaa.json"] = mustJSON(t, vella)

	ana := loginStudent(t, fake, "ana")
	if err := ana.SendPeerMessage("beto", "mensaxe nova", nil); err != nil {
		t.Fatalf("SendPeerMessage: %v", err)
	}
	got, err := ana.GetPeerMessages("beto")
	if err != nil {
		t.Fatalf("GetPeerMessages: %v", err)
	}
	if len(got) != 2 || got[0].Text != "mensaxe vella" || got[1].Text != "mensaxe nova" {
		t.Fatalf("o fío debería mesturar o legado e o novo, por orde: %+v", got)
	}
}

func TestMessagingDisabledByAdmin(t *testing.T) {
	fake := seedGroup(t)
	// Ana is individually disabled; the whole group bac2 is disabled; wall/peer off.
	fake.files["dixitalizacion/mensaxes-config.json"] = mustJSON(t, MessagingConfig{
		WallEnabled: false, PeerEnabled: false,
		DisabledGroups: []string{"bac2"}, DisabledUsers: []string{"ana"},
	})
	ana := loginStudent(t, fake, "ana")
	beto := loginStudent(t, fake, "beto")
	cora := loginStudent(t, fake, "cora")

	// A disabled user cannot send anything (not even to the professor).
	if err := ana.SendMessageToProfessor("mat1", "hola", nil); err == nil {
		t.Fatal("un usuario desactivado non debería poder escribir ao profesor")
	}
	// A disabled group cannot send.
	if err := cora.SendMessageToProfessor("mat1", "hola", nil); err == nil {
		t.Fatal("un usuario dun grupo desactivado non debería poder escribir")
	}
	// Wall and peer are globally off.
	if err := beto.PostToWall("eso4a", "aviso", nil); err == nil {
		t.Fatal("o muro está desactivado; non debería aceptar mensaxes")
	}
	if err := beto.SendPeerMessage("ana", "ola", nil); err == nil {
		t.Fatal("o 1:1 está desactivado; non debería aceptar mensaxes")
	}
}

func TestSaveMessagingConfigRequiresAdmin(t *testing.T) {
	a, _ := loginAs(t, "prof", true, nil)
	if err := a.SaveMessagingConfig(defaultMessagingConfig()); err == nil {
		t.Fatal("gardar a config de mensaxería debería requirir a conta de administración")
	}
	// GetMessagingConfig returns the defaults when no file exists yet.
	cfg, err := a.GetMessagingConfig()
	if err != nil {
		t.Fatalf("GetMessagingConfig: %v", err)
	}
	if !cfg.WallEnabled || !cfg.PeerEnabled {
		t.Fatalf("por defecto o servizo debería estar activo: %+v", cfg)
	}
}

func TestDownloadChatAttachmentRejectsEscape(t *testing.T) {
	a, _ := loginAs(t, "alumno", false, nil)
	if _, err := a.DownloadChatAttachment("../../etc/passwd", "id", "x"); err == nil {
		t.Fatal("unha ruta base que escapa debería rexeitarse")
	}
	if _, err := a.DownloadChatAttachment("dixitalizacion/materias", "id", "x"); err == nil {
		t.Fatal("unha ruta base fóra de mensaxes debería rexeitarse")
	}
	if _, err := a.DownloadChatAttachment("users/ana/mensaxes/m1", "../..", "x"); err == nil {
		t.Fatal("un msgID con traversal debería rexeitarse")
	}
}
