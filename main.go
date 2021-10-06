package main

import(
  "fmt"
  "log"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"

  "github.com/zenhack/go.notmuch"
  cp "github.com/nmrshll/go-cp"
)

var MailDir string = MyHomeDir() + "/.mail/"
const SubFolder = "vrvis"
const ArchiveFolder = "Archive"

var IgnoreTags = []string{"unread", "attachment", "signed", "replied", "archives", "flagged", "important"}

func MyHomeDir() string {
  homeDir, _ := os.UserHomeDir()
  return homeDir
}

func ShouldIgnoreTag(tag string) bool {
  for _, t := range IgnoreTags {
    if t == tag {
      return true
    }
  }
  return false
}

func CreateMaildir(maildir string) {
  fullpath := fmt.Sprintf("%s/%s", MailDir, maildir)
  fmt.Println("creating " + fullpath)
  // FIXME: only create what's needed
  os.Mkdir(fullpath, 0755)
  os.Mkdir(fullpath + "/cur", 0755)
  os.Mkdir(fullpath + "/new", 0755)
  os.Mkdir(fullpath + "/tmp", 0755)
}

// This is actually in the go.notmuch module but doesn't seem exported...
// FIXME: this should only look at mails in SubFolder
func TagList(db *notmuch.DB) []string {
  tags, _ := db.Tags()

  var t *notmuch.Tag
  output := []string{}
  for tags.Next(&t) {
    if !ShouldIgnoreTag(t.Value) {
      output = append(output, t.Value)
    }
  }
  return output
}

func MsgFilenames(msg *notmuch.Message) []string {
  filenames := msg.Filenames()

  var fn string
  output := []string{}
  for filenames.Next(&fn) {
    //fmt.Println(fn)
    output = append(output, fn)
  }
  return output
}

func Tag2maildir(tag string) string {
  // Some tags can have explicit folder mappings
  switch tag {
    case "inbox":
      return "Inbox"
    case "trash":
      return "Trash"
    case "sent":
      return "Sent"
  }

  // everything gets regex manipulated
  r, _ := regexp.Compile("/")
  folder := r.ReplaceAllString(tag, ".")
  return folder
}

func Maildir2tag(folder string) string {
  // Some folders can have explicit mappings
  switch folder {
    case "Inbox":
      return "inbox"
    case "Trash":
      return "trash"
    case "Sent":
      return "sent"
  }

  // everything gets regex manipulated
  r, _ := regexp.Compile("\\.")
  tag := r.ReplaceAllString(folder, "/")
  return tag
}

func CopyMessage(db *notmuch.DB, msg *notmuch.Message, folder string) string {
  destFolder := fmt.Sprintf("%s/%s/%s/cur", MailDir, SubFolder, folder)

  // FIXME: only create if needed
  CreateMaildir(destFolder)

  r, _ := regexp.Compile(",.*")
  nonUIDfn := r.ReplaceAllString(filepath.Base(msg.Filename()), "")
  destPath := fmt.Sprintf("%s/%s", destFolder, nonUIDfn)


  // FIXME: there may be more than one filename, make sure we get one that's wrong
  // for now die if there's more than one path to a message
  if len(MsgFilenames(msg)) > 1 {
    log.Fatal(fmt.Sprintf("message at paths %#v has too many filenames to handle", MsgFilenames(msg)))
  }

  oldPath := msg.Filename()
  fmt.Printf("\u001B[32m" + "copying message id %s from %s to %s\n" + "\u001B[0m", msg.ID(), oldPath, destPath)

  err := cp.CopyFile(oldPath, destPath)
  if err != nil {
    log.Fatal(err)
  }
  db.AddMessage(destPath)

  return oldPath
}

// make sure all mail for the given tag is in a proper folder
func EnsureFolderTag(db *notmuch.DB, tag string) []string {
  rmPaths := []string{}

  tagFolder := Tag2maildir(tag)

  querystring := fmt.Sprintf("folder:/^%s/ and NOT folder:%s/%s and tag:%s", 
                             SubFolder, SubFolder, tagFolder, tag)
  //fmt.Println(querystring)
  msgs, err := db.NewQuery(querystring).Messages()
  if err != nil {
    log.Fatal(err)
  }

  var msg *notmuch.Message
  for msgs.Next(&msg) {
    rmPath := CopyMessage(db, msg, tagFolder)
    rmPaths = append(rmPaths, rmPath)
  }

  return rmPaths
}

func ArchiveUntagged(db *notmuch.DB) []string {
  rmPaths := []string{}

  var tags = TagList(db)

  querystring := fmt.Sprintf("folder:/^%s/ and NOT folder:%s/%s", SubFolder, SubFolder, ArchiveFolder)
  for _,t := range tags {
    querystring += " and NOT tag:" + t
  }

  msgs, err := db.NewQuery(querystring).Messages()
  if err != nil {
    log.Fatal(err)
  }

  var msg *notmuch.Message
  for msgs.Next(&msg) {
    rmPath := CopyMessage(db, msg, ArchiveFolder)
    rmPaths = append(rmPaths, rmPath)
  }

  return rmPaths
}

func CleanMessages(db *notmuch.DB, msgPaths []string) {
  for _, msgPath := range msgPaths {
    fmt.Println("\u001B[31m" + "removing message path: " + msgPath + "\u001B[0m")
    err := os.Remove(msgPath)
    if err != nil {
      log.Fatal(err)
    }
    db.RemoveMessage(msgPath)
  }
}

func TagsToFolders(db *notmuch.DB) []string {
  rmPaths := []string{}

  for _,tag := range TagList(db) {
    //fmt.Println("processing tag " + tag)
    rmPaths = append(rmPaths, EnsureFolderTag(db, tag)...)
  }

  return rmPaths
}

func main() {
  db,err := notmuch.Open(MailDir, notmuch.DBReadWrite)
  if err != nil {
    log.Fatal(err)
  }
  defer db.Close()

  //EnsureFolderTag(db, "payslip")
  rmPaths := []string{}
  rmPaths = append(rmPaths,  TagsToFolders(db)...)
  rmPaths = append(rmPaths, ArchiveUntagged(db)...)

  // clean after copying so we don't accidentally delete the one source message
  CleanMessages(db, rmPaths)

  db.Close()

  // Need one more indexing to be sure everything is captured
  cmd := exec.Command("notmuch", "new")
  err  = cmd.Run()
  if err != nil {
    log.Fatal(err)
  }
}

