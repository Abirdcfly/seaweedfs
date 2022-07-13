package shell

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"github.com/chrislusf/seaweedfs/weed/filer"
	"io"
	"sort"
	"strings"

	"github.com/chrislusf/seaweedfs/weed/pb/filer_pb"
	"github.com/chrislusf/seaweedfs/weed/pb/iam_pb"
)

func init() {
	Commands = append(Commands, &commandS3Configure{})
}

type commandS3Configure struct {
}

func (c *commandS3Configure) Name() string {
	return "s3.configure"
}

func (c *commandS3Configure) Help() string {
	return `configure and apply s3 options for each bucket

	# see the current configuration file content
	s3.configure
	`
}

func (c *commandS3Configure) Do(args []string, commandEnv *CommandEnv, writer io.Writer) (err error) {

	s3ConfigureCommand := flag.NewFlagSet(c.Name(), flag.ContinueOnError)
	actions := s3ConfigureCommand.String("actions", "", "comma separated actions names: Read,Write,List,Tagging,Admin")
	user := s3ConfigureCommand.String("user", "", "user name")
	buckets := s3ConfigureCommand.String("buckets", "", "bucket name")
	accessKey := s3ConfigureCommand.String("access_key", "", "specify the access key")
	secretKey := s3ConfigureCommand.String("secret_key", "", "specify the secret key")
	isDelete := s3ConfigureCommand.Bool("delete", false, "delete users, actions or access keys")
	apply := s3ConfigureCommand.Bool("apply", false, "update and apply s3 configuration")
	if err = s3ConfigureCommand.Parse(args); err != nil {
		return nil
	}

	var buf bytes.Buffer
	if err = commandEnv.WithFilerClient(false, func(client filer_pb.SeaweedFilerClient) error {
		return filer.ReadEntry(commandEnv.MasterClient, client, filer.IamConfigDirecotry, filer.IamIdentityFile, &buf)
	}); err != nil && err != filer_pb.ErrNotFound {
		return err
	}

	s3cfg := &iam_pb.S3ApiConfiguration{}
	if buf.Len() > 0 {
		if err = filer.ParseS3ConfigurationFromBytes(buf.Bytes(), s3cfg); err != nil {
			return err
		}
	}

	idx := 0
	changed := false
	if *user != "" {
		for i, identity := range s3cfg.Identities {
			if *user == identity.Name {
				idx = i
				changed = true
				break
			}
		}
	}
	var cmdActions []string
	for _, action := range strings.Split(*actions, ",") {
		if *buckets == "" {
			cmdActions = append(cmdActions, action)
		} else {
			for _, bucket := range strings.Split(*buckets, ",") {
				cmdActions = append(cmdActions, fmt.Sprintf("%s:%s", action, bucket))
			}
		}
	}
	if changed {
		infoAboutSimulationMode(writer, *apply, "-apply")
		if *isDelete {
			var exists []int
			for _, cmdAction := range cmdActions {
				for i, currentAction := range s3cfg.Identities[idx].Actions {
					if cmdAction == currentAction {
						exists = append(exists, i)
					}
				}
			}
			sort.Sort(sort.Reverse(sort.IntSlice(exists)))
			for _, i := range exists {
				s3cfg.Identities[idx].Actions = append(
					s3cfg.Identities[idx].Actions[:i],
					s3cfg.Identities[idx].Actions[i+1:]...,
				)
			}
			if *accessKey != "" {
				exists = []int{}
				for i, credential := range s3cfg.Identities[idx].Credentials {
					if credential.AccessKey == *accessKey {
						exists = append(exists, i)
					}
				}
				sort.Sort(sort.Reverse(sort.IntSlice(exists)))
				for _, i := range exists {
					s3cfg.Identities[idx].Credentials = append(
						s3cfg.Identities[idx].Credentials[:i],
						s3cfg.Identities[idx].Credentials[:i+1]...,
					)
				}

			}
			if *actions == "" && *accessKey == "" && *buckets == "" {
				s3cfg.Identities = append(s3cfg.Identities[:idx], s3cfg.Identities[idx+1:]...)
			}
		} else {
			if *actions != "" {
				for _, cmdAction := range cmdActions {
					found := false
					for _, action := range s3cfg.Identities[idx].Actions {
						if cmdAction == action {
							found = true
							break
						}
					}
					if !found {
						s3cfg.Identities[idx].Actions = append(s3cfg.Identities[idx].Actions, cmdAction)
					}
				}
			}
			if *accessKey != "" && *user != "anonymous" {
				found := false
				for _, credential := range s3cfg.Identities[idx].Credentials {
					if credential.AccessKey == *accessKey {
						found = true
						credential.SecretKey = *secretKey
						break
					}
				}
				if !found {
					s3cfg.Identities[idx].Credentials = append(s3cfg.Identities[idx].Credentials, &iam_pb.Credential{
						AccessKey: *accessKey,
						SecretKey: *secretKey,
					})
				}
			}
		}
	} else if *user != "" && *actions != "" {
		infoAboutSimulationMode(writer, *apply, "-apply")
		identity := iam_pb.Identity{
			Name:        *user,
			Actions:     cmdActions,
			Credentials: []*iam_pb.Credential{},
		}
		if *user != "anonymous" {
			identity.Credentials = append(identity.Credentials,
				&iam_pb.Credential{AccessKey: *accessKey, SecretKey: *secretKey})
		}
		s3cfg.Identities = append(s3cfg.Identities, &identity)
	}

	accessKeySet := make(map[string]string)
	for _, ident := range s3cfg.Identities {
		for _, cred := range ident.Credentials {
			if userName, found := accessKeySet[cred.AccessKey]; !found {
				accessKeySet[cred.AccessKey] = ident.Name
			} else {
				return errors.New(fmt.Sprintf("duplicate accessKey[%s], already configured in user[%s]", cred.AccessKey, userName))
			}
		}
	}

	buf.Reset()
	filer.ProtoToText(&buf, s3cfg)

	fmt.Fprintf(writer, string(buf.Bytes()))
	fmt.Fprintln(writer)

	if *apply {

		if err := commandEnv.WithFilerClient(false, func(client filer_pb.SeaweedFilerClient) error {
			return filer.SaveInsideFiler(client, filer.IamConfigDirecotry, filer.IamIdentityFile, buf.Bytes())
		}); err != nil {
			return err
		}

	}

	return nil
}
