package grpcmock

import (
	"errors"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// serveReflection answers grpc.reflection.v1 from the suite's own protos, so a
// client such as grpcurl lists and calls the mocked methods without a .proto of
// its own. Only the services the suite mocks are listed.
func serveReflection(stream grpc.ServerStream, index *Index) error {
	for {
		request := new(rpb.ServerReflectionRequest)
		if err := stream.RecvMsg(request); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		response := &rpb.ServerReflectionResponse{ValidHost: request.GetHost(), OriginalRequest: request}

		switch typed := request.GetMessageRequest().(type) {
		case *rpb.ServerReflectionRequest_ListServices:
			services := make([]*rpb.ServiceResponse, 0)
			for _, name := range index.services() {
				services = append(services, &rpb.ServiceResponse{Name: name})
			}
			response.MessageResponse = &rpb.ServerReflectionResponse_ListServicesResponse{
				ListServicesResponse: &rpb.ListServiceResponse{Service: services},
			}
		case *rpb.ServerReflectionRequest_FileByFilename:
			answerWithFile(response, index.byPath[typed.FileByFilename], "file "+typed.FileByFilename+" not found")
		case *rpb.ServerReflectionRequest_FileContainingSymbol:
			answerWithFile(response, index.fileBySymbol(typed.FileContainingSymbol), "symbol "+typed.FileContainingSymbol+" not found")
		default:
			answerWithError(response, codes.Unimplemented, "this reflection request is not supported")
		}
		if err := stream.SendMsg(response); err != nil {
			return err
		}
	}
}

func (i *Index) fileBySymbol(symbol string) protoreflect.FileDescriptor {
	for _, file := range i.byPath {
		if declares(file, protoreflect.FullName(symbol)) {
			return file
		}
	}
	return nil
}

// declares reports whether the file declares a service, method or message by
// that full name.
func declares(file protoreflect.FileDescriptor, name protoreflect.FullName) bool {
	services := file.Services()
	for s := 0; s < services.Len(); s++ {
		service := services.Get(s)
		if service.FullName() == name {
			return true
		}
		methods := service.Methods()
		for m := 0; m < methods.Len(); m++ {
			if methods.Get(m).FullName() == name {
				return true
			}
		}
	}
	messages := file.Messages()
	for m := 0; m < messages.Len(); m++ {
		if messages.Get(m).FullName() == name {
			return true
		}
	}
	return false
}

// answerWithFile puts a file and everything it imports into the response, as the
// protocol requires, so the client can resolve every type.
func answerWithFile(response *rpb.ServerReflectionResponse, file protoreflect.FileDescriptor, notFound string) {
	if file == nil {
		answerWithError(response, codes.NotFound, notFound)
		return
	}
	response.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
		FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: closure(file)},
	}
}

func answerWithError(response *rpb.ServerReflectionResponse, code codes.Code, message string) {
	response.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
		ErrorResponse: &rpb.ErrorResponse{ErrorCode: int32(code), ErrorMessage: message},
	}
}

// closure returns the serialized descriptors of a file and its transitive imports.
func closure(file protoreflect.FileDescriptor) [][]byte {
	seen := map[string]bool{}
	var out [][]byte
	var walk func(protoreflect.FileDescriptor)
	walk = func(current protoreflect.FileDescriptor) {
		if seen[current.Path()] {
			return
		}
		seen[current.Path()] = true
		if raw, err := proto.Marshal(protodesc.ToFileDescriptorProto(current)); err == nil {
			out = append(out, raw)
		}
		imports := current.Imports()
		for i := 0; i < imports.Len(); i++ {
			walk(imports.Get(i).FileDescriptor)
		}
	}
	walk(file)
	return out
}
