package namespace

import (
	"bytes"
	"context"
	"fmt"
	"time"

	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cert-manager/istio-csr/test/e2e/framework"
)

var _ = framework.CasesDescribe("CA Root Controller", func() {
	f := framework.NewDefaultFramework("ca-root-controller")

	var (
		testName    = "cert-manager-istio-agent-e2e-root-ca"
		cmNamespace = "istio-system"
		ctx         = context.Background()
		rootCA      []byte
	)

	JustBeforeEach(func() {
		By("collecting the current root CA which should be propagated")

		// Get root CA from a dummy Certificate using configured issuer
		cert := &cmapi.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cert-manager-istio-agent-e2e-",
				Namespace:    cmNamespace,
			},
			Spec: cmapi.CertificateSpec{
				CommonName: testName,
				IssuerRef:  f.Config().IssuerRef,
				SecretName: testName,
			},
		}

		cert, err := f.CMClientSet.CertmanagerV1().Certificates(cmNamespace).Create(ctx, cert, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err = f.Helper().WaitForCertificateReady(cmNamespace, cert.Name, time.Second*10)
		Expect(err).NotTo(HaveOccurred())

		certSecret, err := f.KubeClientSet.CoreV1().Secrets(cmNamespace).Get(ctx, testName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ok bool
		rootCA, ok = certSecret.Data[cmmeta.TLSCAKey]
		if !ok || len(rootCA) == 0 {
			Expect(certSecret, "failed to find root CA key in test certificate secret").NotTo(HaveOccurred())
		}
	})

	It("all namespaces should have valid configs in", func() {
		By("ensure all existing namespaces have the correct root CA")
		nss, err := f.KubeClientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		for _, ns := range nss.Items {
			Expect(expectRootCAExists(ctx, f, ns.Name, rootCA)).NotTo(HaveOccurred())
		}
	})

	It("should correctly update when a namespace updates and config map changes", func() {
		By("ensure a newly namespace is propagated")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cert-manager-istio-agent-e2e-",
			},
		}

		ns, err := f.KubeClientSet.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		defer func() {
			By("removing test namespace")
			Expect(f.KubeClientSet.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})).NotTo(HaveOccurred())
		}()

		Expect(expectRootCAExists(ctx, f, ns.Name, rootCA)).NotTo(HaveOccurred())

		By("if the config map contents is overridden, it should revert the changes")
		cm, err := f.KubeClientSet.CoreV1().ConfigMaps(ns.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		cm.Data[cmmeta.TLSCAKey] = "foo-bar"

		cm, err = f.KubeClientSet.CoreV1().ConfigMaps(ns.Name).Update(ctx, cm, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(expectRootCAExists(ctx, f, ns.Name, rootCA)).NotTo(HaveOccurred())

		By("if the config map contents is deleted, it should revert the changes")
		cm, err = f.KubeClientSet.CoreV1().ConfigMaps(ns.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		delete(cm.Data, cmmeta.TLSCAKey)

		cm, err = f.KubeClientSet.CoreV1().ConfigMaps(ns.Name).Update(ctx, cm, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(expectRootCAExists(ctx, f, ns.Name, rootCA)).NotTo(HaveOccurred())
	})
})

func expectRootCAExists(ctx context.Context, f *framework.Framework, ns string, rootCA []byte) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	for {
		cm, err := f.KubeClientSet.CoreV1().ConfigMaps(ns).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})

		if err == nil {
			if data, ok := cm.Data["root-cert.pem"]; !ok || !bytes.Equal([]byte(data), rootCA) {
				err = fmt.Errorf("%+#v: expected root CA not present in ConfigMap", cm)
			}
		}

		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return err
		default:
			time.Sleep(time.Millisecond * 100)
			continue
		}
	}

	return nil
}
